package repo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	return &Manager{
		Store:     Store{Root: filepath.Join(home, "repositories")},
		Validator: v,
		Getenv: func(k string) string {
			return map[string]string{"HOME": home, "PATH": os.Getenv("PATH")}[k]
		},
		LookPath: exec.LookPath,
		Now:      func() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) },
	}
}

func manifest(id, version string, perms string) string {
	m := `{"id":"` + id + `","version":"` + version + `","name":"` + id + `","category":"tool"`
	if perms != "" {
		m += `,"permissions":` + perms
	}
	return m + `}`
}

func entryFor(t *testing.T, name string, fields map[string]any) Entry {
	t.Helper()
	e := Collect([]Layer{{Origin: OriginUser, Repositories: map[string]any{name: fields}}}, nil)[0]
	if err := e.Problem(); err != nil {
		t.Fatal(err)
	}
	return e
}

func viewOf(t *testing.T, m *Manager, e Entry) View {
	t.Helper()
	return m.View(e)
}

func findPlugin(t *testing.T, plugins []PluginView, id string) PluginView {
	t.Helper()
	for _, p := range plugins {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no plugin %s in %v", id, plugins)
	return PluginView{}
}

func newPermissions(p PluginView) []string {
	var out []string
	for _, perm := range p.Permissions {
		if perm.New {
			out = append(out, perm.Kind+"="+perm.Value)
		}
	}
	return out
}

// TestSyncLifecycle runs a local directory through sync, approval, a second revision
// with a new permission, discard, and removal.
func TestSyncLifecycle(t *testing.T) {
	ctx := context.Background()
	m := testManager(t)
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"plugins/alpha/plugin.json":  manifest("alpha", "1.0.0", `{"network":["https://downloads.example.com"]}`),
		"plugins/beta/plugin.json":   manifest("beta", "1.0.0", ""),
		"plugins/gone/plugin.json":   manifest("gone", "1.0.0", ""),
		"plugins/broken/plugin.json": `{"id":"broken","version":"x"}`,
		"plugins/notaplugin/readme":  "no manifest",
	})
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": "file://" + src, "path": "plugins"})

	if v := viewOf(t, m, e); v.State != StateNew {
		t.Fatalf("state before the first sync = %s", v.State)
	}
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	v := viewOf(t, m, e)
	if v.State != StatePending || v.Active != nil || v.Pending == nil || v.LastSync == nil || !v.LastSync.Changed {
		t.Fatalf("after the first sync: %+v", v)
	}
	if ids := strings.Join(v.PluginIDs(), ","); ids != "alpha,beta,broken,gone" {
		t.Errorf("plugins = %s", ids)
	}
	alpha := findPlugin(t, v.Pending.Plugins, "alpha")
	if alpha.Change != ChangeAdded || len(newPermissions(alpha)) != 1 {
		t.Errorf("alpha = %+v", alpha)
	}
	if broken := findPlugin(t, v.Pending.Plugins, "broken"); broken.Error == "" || strings.Contains(broken.Error, m.Store.Root) {
		t.Errorf("broken error = %q", broken.Error)
	}
	if roots := m.Roots([]Entry{e}); len(roots) != 0 {
		t.Errorf("a pending revision must not be loaded: %v", roots)
	}

	// the same revision again changes nothing
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	if v2 := viewOf(t, m, e); v2.LastSync.Changed || v2.Pending.Revision != v.Pending.Revision {
		t.Errorf("a second sync of the same revision: %+v", v2.LastSync)
	}

	// approval needs the pending revision
	if err := m.Approve(ctx, e, strings.Repeat("0", 64)); !config.IsCode(err, config.CodeConflict) {
		t.Errorf("approve with a wrong revision: %v", err)
	}
	if err := m.Approve(ctx, e, v.Pending.Revision); err != nil {
		t.Fatal(err)
	}
	if err := m.Approve(ctx, e, v.Pending.Revision); !config.IsCode(err, config.CodeConflict) {
		t.Errorf("approve without a pending revision: %v", err)
	}
	active := viewOf(t, m, e)
	if active.State != StateActive || active.Active == nil || active.Pending != nil {
		t.Fatalf("after approval: %+v", active)
	}
	roots := m.Roots([]Entry{e})
	if len(roots) != 1 || roots[0].Origin.Repository != "team" || roots[0].Origin.Revision != ShortRevision(v.Pending.Revision) {
		t.Fatalf("roots = %+v", roots)
	}
	if _, err := os.Stat(filepath.Join(roots[0].Dir, "alpha", "plugin.json")); err != nil {
		t.Error(err)
	}

	// second revision: alpha gets a permission, beta is untouched, gone is removed, new is added
	writeTree(t, src, map[string]string{
		"plugins/alpha/plugin.json": manifest("alpha", "1.1.0", `{"network":["https://downloads.example.com"],"sudo":true,"files":{"write":["${env:HOME}/.alpharc"]}}`),
		"plugins/new/plugin.json":   manifest("new", "0.1.0", `{"credentials":["registry"]}`),
	})
	if err := os.RemoveAll(filepath.Join(src, "plugins", "gone")); err != nil {
		t.Fatal(err)
	}
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	v = viewOf(t, m, e)
	if v.State != StatePending || v.Active == nil || v.Pending == nil || v.Active.Revision == v.Pending.Revision {
		t.Fatalf("after the second revision: %+v", v)
	}
	if roots := m.Roots([]Entry{e}); len(roots) != 1 || roots[0].Origin.Revision != ShortRevision(v.Active.Revision) {
		t.Errorf("the active revision stays in use: %+v", roots)
	}
	tests := []struct {
		id, change, previous string
		newPerms             []string
	}{
		{"alpha", ChangeChanged, "1.0.0", []string{"files.write=${env:HOME}/.alpharc", "sudo=true"}},
		{"beta", ChangeUnchanged, "", nil},
		{"gone", ChangeRemoved, "", nil},
		{"new", ChangeAdded, "", []string{"credentials=registry"}},
	}
	for _, tt := range tests {
		p := findPlugin(t, v.Pending.Plugins, tt.id)
		if p.Change != tt.change || p.PreviousVersion != tt.previous || strings.Join(newPermissions(p), ";") != strings.Join(tt.newPerms, ";") {
			t.Errorf("%s: change %q, previous %q, new permissions %v", tt.id, p.Change, p.PreviousVersion, newPermissions(p))
		}
	}

	// discard keeps the active revision, and the pending files are gone
	pendingDir := m.Store.Snapshot("team", ShortRevision(v.Pending.Revision))
	if err := m.Discard(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := m.Discard(ctx, e); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("discard without a pending revision: %v", err)
	}
	if v = viewOf(t, m, e); v.State != StateActive || v.Pending != nil {
		t.Errorf("after discard: %+v", v)
	}
	if _, err := os.Stat(pendingDir); err == nil {
		t.Error("the files of the discarded revision are still there")
	}

	// sync again, approve: the older revision is deleted
	oldDir := m.Store.Snapshot("team", ShortRevision(v.Active.Revision))
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	pending := viewOf(t, m, e).Pending.Revision
	if err := m.Approve(ctx, e, pending); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldDir); err == nil {
		t.Error("the older revision directory survived the approval")
	}

	if err := m.Store.Remove(ctx, "team"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Store.Dir("team")); err == nil {
		t.Error("the repository directory survived removal")
	}
}

func TestSyncErrors(t *testing.T) {
	ctx := context.Background()
	m := testManager(t)
	src := t.TempDir()
	writeTree(t, src, map[string]string{"a/plugin.json": manifest("alpha", "1.0.0", "")})
	if err := os.Symlink("/etc/hostname", filepath.Join(src, "a", "link")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		fields map[string]any
		code   string
	}{
		{"link in the source", map[string]any{"kind": "plugins", "source": "file://" + src}, config.CodeValidation},
		{"missing directory", map[string]any{"kind": "plugins", "source": "file://" + filepath.Join(src, "nope")}, config.CodeValidation},
		{"credential", map[string]any{"kind": "plugins", "source": "file://" + src, "credential": "x"}, config.CodeValidation},
		{"disabled", map[string]any{"kind": "plugins", "source": "file://" + src, "enabled": false}, config.CodeConflict},
		{"other kind", map[string]any{"kind": "templates", "source": "file://" + src}, config.CodeConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Collect([]Layer{{Origin: OriginUser, Repositories: map[string]any{"some-repo": tt.fields}}}, nil)[0]
			if err := m.Sync(ctx, e); !config.IsCode(err, tt.code) {
				t.Errorf("error = %v, want code %s", err, tt.code)
			}
		})
	}

	// a failed sync is recorded and keeps the active revision
	good := t.TempDir()
	writeTree(t, good, map[string]string{"a/plugin.json": manifest("alpha", "1.0.0", "")})
	e := entryFor(t, "keeps-active", map[string]any{"kind": "plugins", "source": "file://" + good})
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := m.Approve(ctx, e, viewOf(t, m, e).Pending.Revision); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hostname", filepath.Join(good, "a", "link")); err != nil {
		t.Fatal(err)
	}
	if err := m.Sync(ctx, e); err == nil {
		t.Fatal("want an error")
	}
	v := viewOf(t, m, e)
	if v.State != StateError || v.LastError == "" || v.Active == nil {
		t.Errorf("after a failed sync: %+v", v)
	}
	if len(m.Roots([]Entry{e})) != 1 {
		t.Error("the active revision must stay loaded after a failed sync")
	}
}

func TestSyncPathMustExist(t *testing.T) {
	m := testManager(t)
	src := t.TempDir()
	writeTree(t, src, map[string]string{"a/plugin.json": manifest("alpha", "1.0.0", "")})
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": "file://" + src, "path": "plugins"})
	if err := m.Sync(context.Background(), e); !config.IsCode(err, config.CodeValidation) {
		t.Errorf("error = %v", err)
	}
}

func TestViewStates(t *testing.T) {
	m := testManager(t)
	src := t.TempDir()
	tests := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"never synced", map[string]any{"kind": "plugins", "source": "file://" + src}, StateNew},
		{"switched off", map[string]any{"kind": "plugins", "source": "file://" + src, "enabled": false}, StateDisabled},
		{"other kind", map[string]any{"kind": "packages"}, StateUnsupported},
		{"invalid entry", map[string]any{"kind": "plugins", "source": "file://" + src, "credential": "x"}, StateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Collect([]Layer{{Origin: OriginUser, Repositories: map[string]any{"some-repo": tt.fields}}}, nil)[0]
			if v := m.View(e); v.State != tt.want {
				t.Errorf("state = %s, want %s", v.State, tt.want)
			}
		})
	}
}

func TestStateIsStrict(t *testing.T) {
	m := testManager(t)
	dir := m.Store.Dir("team")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, StateFile), []byte(`{"unknown": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Store.ReadState(m.Validator, "team"); err == nil {
		t.Error("unknown keys in state.json must be an error")
	}
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": "file:///srv/x"})
	if v := m.View(e); v.State != StateError || v.LastError == "" {
		t.Errorf("view = %+v", v)
	}
	if roots := m.Roots([]Entry{e}); len(roots) != 0 {
		t.Errorf("roots = %v", roots)
	}
}

func tarballServer(t *testing.T, body []byte, status int) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func TestSyncHTTPS(t *testing.T) {
	ctx := context.Background()
	archive := writeTarGz(t, []tarEntry{
		{name: "repo-main/plugins/alpha/plugin.json", body: manifest("alpha", "1.0.0", "")},
		{name: "repo-main/plugins/alpha/run.sh", body: "#!/bin/sh\n", mode: 0o755},
	})
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	srv, client := tarballServer(t, body, http.StatusOK)
	m := testManager(t)
	m.Client = client
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": srv.URL + "/repo.tar.gz", "path": "plugins"})
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	v := m.View(e)
	if v.State != StatePending || len(v.Pending.Revision) != 64 {
		t.Fatalf("view = %+v", v)
	}
	if err := m.Approve(ctx, e, v.Pending.Revision); err != nil {
		t.Fatal(err)
	}
	roots := m.Roots([]Entry{e})
	info, err := os.Stat(filepath.Join(roots[0].Dir, "alpha", "run.sh"))
	if err != nil || !executable(info.Mode()) {
		t.Errorf("run.sh: %v, %v", info, err)
	}
}

func TestSyncHTTPSFailures(t *testing.T) {
	ctx := context.Background()
	evil := writeTarGz(t, []tarEntry{{name: "../evil", body: "x"}})
	evilBody, err := os.ReadFile(evil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		body   []byte
		status int
		code   string
	}{
		{"server error", []byte("boom"), http.StatusInternalServerError, config.CodePrerequisite},
		{"not an archive", []byte("plain text"), http.StatusOK, config.CodeValidation},
		{"path traversal", evilBody, http.StatusOK, config.CodeValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, client := tarballServer(t, tt.body, tt.status)
			m := testManager(t)
			m.Client = client
			e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": srv.URL + "/repo.tar.gz"})
			if err := m.Sync(ctx, e); !config.IsCode(err, tt.code) {
				t.Errorf("error = %v, want code %s", err, tt.code)
			}
			if v := m.View(e); v.State != StateError || v.LastError == "" {
				t.Errorf("view = %+v", v)
			}
		})
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...) //nolint:gosec // git with fixed test arguments
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSyncGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	src := t.TempDir()
	git(t, src, "init", "--quiet", "--initial-branch=main")
	writeTree(t, src, map[string]string{"plugins/alpha/plugin.json": manifest("alpha", "1.0.0", "")})
	git(t, src, "add", ".")
	git(t, src, "commit", "--quiet", "-m", "one")
	first := git(t, src, "rev-parse", "HEAD")
	url := "git+file://" + src

	// default ref: prompt trust, so the revision is pending, and .git is not kept
	m := testManager(t)
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": url, "path": "plugins"})
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	v := m.View(e)
	if v.State != StatePending || v.Pending.Revision != first {
		t.Fatalf("view = %+v, want pending %s", v, first)
	}
	if _, err := os.Stat(filepath.Join(m.Store.Snapshot("team", ShortRevision(first)), ".git")); err == nil {
		t.Error(".git must not stay in the snapshot")
	}

	// a branch
	pinnedByBranch := entryFor(t, "by-branch", map[string]any{"kind": "plugins", "source": url, "ref": "main", "path": "plugins"})
	if err := m.Sync(ctx, pinnedByBranch); err != nil {
		t.Fatal(err)
	}

	// a second commit; pinned to the first one stays active without approval
	writeTree(t, src, map[string]string{"plugins/beta/plugin.json": manifest("beta", "1.0.0", "")})
	git(t, src, "add", ".")
	git(t, src, "commit", "--quiet", "-m", "two")
	pinned := entryFor(t, "pinned", map[string]any{"kind": "plugins", "source": url, "ref": first, "trust": "pinned", "path": "plugins"})
	if err := m.Sync(ctx, pinned); err != nil {
		t.Fatal(err)
	}
	pv := m.View(pinned)
	if pv.State != StateActive || pv.Active.Revision != first || pv.Pending != nil || len(pv.Active.Plugins) != 1 {
		t.Fatalf("pinned view = %+v", pv)
	}

	// an unknown ref is an error that names Git
	bad := entryFor(t, "bad-ref", map[string]any{"kind": "plugins", "source": url, "ref": "no-such-branch"})
	if err := m.Sync(ctx, bad); !config.IsCode(err, config.CodePrerequisite) {
		t.Errorf("unknown ref: %v", err)
	}

	// without git in PATH the error is a missing prerequisite
	m.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if err := m.Sync(ctx, e); !config.IsCode(err, config.CodePrerequisite) || !strings.Contains(err.Error(), "git is needed") {
		t.Errorf("without git: %v", err)
	}
}
