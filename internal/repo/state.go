package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// StateFile is the name of the state file in the directory of a repository.
const StateFile = "state.json"

// shortLen is the length of the revision in directory names and in the origin of
// plugins.
const shortLen = 12

// lockPoll is how often a waiting process tries the lock of a repository again.
const lockPoll = 50 * time.Millisecond

// State is the content of state.json (schema/repository-state.v1.json).
type State struct {
	Schema    string    `json:"$schema,omitempty"`
	Active    *Revision `json:"active,omitempty"`
	Pending   *Revision `json:"pending,omitempty"`
	LastSync  *LastSync `json:"lastSync,omitempty"`
	LastError string    `json:"lastError,omitempty"`
}

// Revision is a fetched revision with the plugins it holds.
type Revision struct {
	Revision  string   `json:"revision"`
	Path      string   `json:"path,omitempty"`
	FetchedAt string   `json:"fetchedAt"`
	Plugins   []Plugin `json:"plugins"`
	// SuggestedPaths is only set when Plugins is empty: the folders of the snapshot
	// that hold plugin folders.
	SuggestedPaths []string `json:"suggestedPaths,omitempty"`
}

// Plugin is a plugin folder of a revision.
type Plugin struct {
	ID               string       `json:"id"`
	Name             string       `json:"name,omitempty"`
	Version          string       `json:"version,omitempty"`
	Description      string       `json:"description,omitempty"`
	Digest           string       `json:"digest"`
	Permissions      []Permission `json:"permissions"`
	RequiresCommands []string     `json:"requiresCommands"`
	Error            string       `json:"error,omitempty"`
}

// Permission kinds.
const (
	PermNetwork     = "network"
	PermFilesRead   = "files.read"
	PermFilesWrite  = "files.write"
	PermSudo        = "sudo"
	PermCredentials = "credentials"
)

// Permission is one declared permission of a plugin.
type Permission struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// LastSync is the result of the last successful sync.
type LastSync struct {
	At      string `json:"at"`
	Changed bool   `json:"changed"`
}

// Short returns the short form of the revision, as used in directory names.
func (r Revision) Short() string { return ShortRevision(r.Revision) }

// ShortRevision returns the first characters of a revision.
func ShortRevision(rev string) string {
	if len(rev) > shortLen {
		return rev[:shortLen]
	}
	return rev
}

// Store is the directory that holds the repositories.
type Store struct {
	// Root is ${xdg:data}/devbox/repositories.
	Root string
}

// Dir is the directory of one repository.
func (s Store) Dir(name string) string { return filepath.Join(s.Root, name) }

// Snapshot is the directory of one revision of a repository.
func (s Store) Snapshot(name, short string) string { return filepath.Join(s.Dir(name), short) }

func (s Store) statePath(name string) string { return filepath.Join(s.Dir(name), StateFile) }

// ReadState reads and validates the state of a repository. A repository that was
// never synced has an empty state.
func (s Store) ReadState(v *schema.Validator, name string) (*State, error) {
	if !ValidName.MatchString(name) {
		return nil, fmt.Errorf("repository name %q is not valid", name)
	}
	path := s.statePath(name)
	data, err := os.ReadFile(path) //nolint:gosec // below the data directory, the name is checked above
	if errors.Is(err, fs.ErrNotExist) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := v.ValidateJSON("repository-state", data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &st, nil
}

// WriteState validates and writes the state of a repository atomically.
func (s Store) WriteState(v *schema.Validator, name string, st *State) error {
	if !ValidName.MatchString(name) {
		return fmt.Errorf("repository name %q is not valid", name)
	}
	if id, ok := v.ID("repository-state"); ok {
		st.Schema = id
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(st); err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := v.ValidateJSON("repository-state", buf.Bytes()); err != nil {
		return fmt.Errorf("state of %s: %w", name, err)
	}
	dir := s.Dir(name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return jsonfile.WriteAtomic(s.statePath(name), buf.Bytes(), 0o600)
}

// lock takes the exclusive lock of a repository directory and waits for it until ctx
// ends. The returned function releases it.
func (s Store) lock(ctx context.Context, name string) (func(), error) {
	dir := s.Dir(name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // below the data directory
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // a file descriptor fits into int
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // a file descriptor fits into int
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("wait for repository %s: %w", name, ctx.Err())
		case <-time.After(lockPoll):
		}
	}
}

// Remove deletes the directory of a repository with all revisions.
func (s Store) Remove(ctx context.Context, name string) error {
	if !ValidName.MatchString(name) {
		return fmt.Errorf("repository name %q is not valid", name)
	}
	unlock, err := s.lock(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()
	return os.RemoveAll(s.Dir(name))
}
