package repo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// SyncTimeout bounds one sync: Git, the download and the scan together.
const SyncTimeout = 5 * time.Minute

// Manager syncs repositories and reads their state.
type Manager struct {
	Store     Store
	Validator *schema.Validator
	// Getenv reads the environment of the call (PATH, HOME, proxy for Git).
	Getenv func(string) string
	// LookPath finds a command in the PATH of the call.
	LookPath func(name string) (string, error)
	// Client downloads tarballs; nil uses the proxy of the environment.
	Client *http.Client
	// Now returns the current time; nil uses the clock.
	Now func() time.Time
}

func (m *Manager) now() string {
	t := time.Now()
	if m.Now != nil {
		t = m.Now()
	}
	return t.UTC().Format(time.RFC3339)
}

func notFoundf(name, format string, a ...any) error {
	return &config.Error{Code: config.CodeNotFound, Key: name, Detail: fmt.Sprintf(format, a...)}
}

func conflictf(name, format string, a ...any) error {
	return &config.Error{Code: config.CodeConflict, Key: name, Detail: fmt.Sprintf(format, a...)}
}

// checkSyncable reports why the entry cannot be synced or approved.
func checkSyncable(e Entry) error {
	switch {
	case !e.Supported():
		return conflictf(e.Name, "repositories of the kind %s are not supported yet", e.Kind)
	case !e.Enabled:
		return conflictf(e.Name, "the repository is switched off")
	}
	return e.Problem()
}

func sameRevision(r *Revision, rev, sub string) bool {
	return r != nil && r.Revision == rev && r.Path == sub
}

// Sync fetches the repository. A new revision becomes pending, or active at once with
// trust "pinned"; the same revision as before changes nothing. The outcome is stored
// in state.json, a failure as lastError (the active revision stays in use).
func (m *Manager) Sync(ctx context.Context, e Entry) error {
	if err := checkSyncable(e); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	unlock, err := m.Store.lock(ctx, e.Name)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := m.Store.ReadState(m.Validator, e.Name)
	if err != nil {
		return fmt.Errorf("the state of %s cannot be read, remove the repository and add it again: %w", e.Name, err)
	}
	rev, plugins, suggested, err := m.fetchAndScan(ctx, e)
	if err != nil {
		if ctx.Err() == nil {
			st.LastError = ErrorText(err)
			if werr := m.Store.WriteState(m.Validator, e.Name, st); werr != nil {
				err = errors.Join(err, werr)
			}
		}
		return err
	}
	ts := m.now()
	sub := e.SubPath()
	fetched := &Revision{Revision: rev, Path: sub, FetchedAt: ts, Plugins: plugins, SuggestedPaths: suggested}
	changed := false
	switch {
	case sameRevision(st.Active, rev, sub):
		changed = st.Pending != nil // the remote went back to the active revision
		st.Pending = nil
		st.Active.SuggestedPaths = suggested // states from before the hint have none
	case sameRevision(st.Pending, rev, sub):
		st.Pending.SuggestedPaths = suggested
	case e.Trust == TrustPinned:
		st.Active, st.Pending, changed = fetched, nil, true
	default:
		st.Pending, changed = fetched, true
	}
	st.LastSync, st.LastError = &LastSync{At: ts, Changed: changed}, ""
	if err := m.Store.WriteState(m.Validator, e.Name, st); err != nil {
		return err
	}
	return m.prune(e.Name, st)
}

// fetchAndScan fetches into a temporary directory, moves it to the directory of its
// revision (unless that exists) and lists the plugins. When there are none, it also
// returns the folders of the snapshot that hold plugin folders.
func (m *Manager) fetchAndScan(ctx context.Context, e Entry) (string, []Plugin, []string, error) {
	tmp, err := os.MkdirTemp(m.Store.Dir(e.Name), ".fetch-")
	if err != nil {
		return "", nil, nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	rev, err := m.fetchInto(ctx, e, tmp)
	if err != nil {
		return "", nil, nil, err
	}
	if _, err := listTree(tmp); err != nil { // links and special files from Git
		return "", nil, nil, err
	}
	final := m.Store.Snapshot(e.Name, ShortRevision(rev))
	if _, err := os.Stat(final); err != nil {
		if err := os.Rename(tmp, final); err != nil {
			return "", nil, nil, err
		}
	}
	dir := subDir(final, e.SubPath())
	if !dirExists(dir) {
		return "", nil, nil, invalidf("the path %q does not exist in revision %s", e.SubPath(), ShortRevision(rev))
	}
	plugins, err := ScanPlugins(m.Validator, dir)
	if err != nil {
		return "", nil, nil, err
	}
	var suggested []string
	if len(plugins) == 0 {
		suggested = SuggestPaths(final, e.SubPath())
	}
	return rev, plugins, suggested, nil
}

// prune removes the revision directories that are neither active nor pending.
func (m *Manager) prune(name string, st *State) error {
	keep := map[string]bool{}
	for _, r := range []*Revision{st.Active, st.Pending} {
		if r != nil {
			keep[r.Short()] = true
		}
	}
	entries, err := os.ReadDir(m.Store.Dir(name))
	if err != nil {
		return err
	}
	for _, de := range entries {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") || keep[de.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.Store.Dir(name), de.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Approve makes the pending revision the active one. revision must be the pending
// revision, so an approval always refers to what the user saw.
func (m *Manager) Approve(ctx context.Context, e Entry, revision string) error {
	if err := checkSyncable(e); err != nil {
		return err
	}
	return m.change(ctx, e.Name, func(st *State) error {
		if st.Pending == nil {
			return conflictf(e.Name, "there is no pending revision to approve")
		}
		if st.Pending.Revision != revision {
			return conflictf(e.Name, "the pending revision is %s, not %s", st.Pending.Revision, revision)
		}
		if !dirExists(m.Store.Snapshot(e.Name, st.Pending.Short())) {
			return conflictf(e.Name, "the files of the pending revision are gone, sync again")
		}
		st.Active, st.Pending = st.Pending, nil
		return nil
	})
}

// Discard drops the pending revision; the active one stays.
func (m *Manager) Discard(ctx context.Context, e Entry) error {
	return m.change(ctx, e.Name, func(st *State) error {
		if st.Pending == nil {
			return notFoundf(e.Name, "there is no pending revision")
		}
		st.Pending = nil
		return nil
	})
}

func (m *Manager) change(ctx context.Context, name string, f func(*State) error) error {
	unlock, err := m.Store.lock(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := m.Store.ReadState(m.Validator, name)
	if err != nil {
		return err
	}
	if err := f(st); err != nil {
		return err
	}
	if err := m.Store.WriteState(m.Validator, name, st); err != nil {
		return err
	}
	return m.prune(name, st)
}

// Roots returns the plugin directories of the active revisions of the enabled
// repositories of kind plugins, in the order of the entries. Entries with a
// configuration problem, without active revision or with an unreadable state are left
// out.
func (m *Manager) Roots(entries []Entry) []plugin.Root {
	var roots []plugin.Root
	for _, e := range entries {
		if !e.Active() || e.Problem() != nil {
			continue
		}
		st, err := m.Store.ReadState(m.Validator, e.Name)
		if err != nil || st.Active == nil {
			continue
		}
		dir := subDir(m.Store.Snapshot(e.Name, st.Active.Short()), st.Active.Path)
		if !dirExists(dir) {
			continue
		}
		roots = append(roots, plugin.Root{Dir: dir, Origin: plugin.Origin{
			Kind: plugin.OriginRepository, Repository: e.Name, Revision: st.Active.Short(),
		}})
	}
	return roots
}

// Names returns the names of the entries, sorted.
func Names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

// ErrorText is the message of an error without the code prefix of a configuration
// error.
func ErrorText(err error) string {
	var ce *config.Error
	if errors.As(err, &ce) {
		return ce.Detail
	}
	return err.Error()
}
