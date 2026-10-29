package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
)

// repoManifest returns a minimal manifest that asks for network access to hosts.
func repoManifest(id, version, category string, hosts ...string) string {
	m := map[string]any{"id": id, "version": version, "name": strings.ToUpper(id[:1]) + id[1:], "category": category}
	if len(hosts) > 0 {
		m["permissions"] = map[string]any{"network": hosts}
	}
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(data)
}

const (
	hostDownloads = "https://downloads.example.com"
	hostCDN       = "https://cdn.example.com"
)

// writeRepoPlugin writes the plugin folder id below dir/plugins.
func writeRepoPlugin(t *testing.T, dir, id, version string, hosts ...string) {
	t.Helper()
	write(t, filepath.Join(dir, "plugins", id, "plugin.json"), repoManifest(id, version, "tool", hosts...), 0o600)
}

// repoSource returns a directory with the plugin folders alpha and beta below plugins/.
func repoSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeRepoPlugin(t, dir, "alpha", "1.0.0", hostDownloads)
	writeRepoPlugin(t, dir, "beta", "1.0.0")
	return dir
}

func (e *testEnv) userConfig() string {
	return filepath.Join(e.home, ".config", "devbox", "config.json")
}

func (e *testEnv) repoDir(name string) string {
	return filepath.Join(e.home, ".local", "share", "devbox", "repositories", name)
}

// writeJSONFile writes v as the content of path.
func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(data), 0o600)
}

// writeSystemConfig replaces the system file; the bundled plugins stay switched off.
func (e *testEnv) writeSystemConfig(extra map[string]any) {
	e.t.Helper()
	doc := map[string]any{"plugins": map[string]any{"disabled": bundled}}
	for k, v := range extra {
		doc[k] = v
	}
	writeJSONFile(e.t, e.getenv[core.SystemConfigEnv], doc)
}

// addProfile writes a profile with the extra keys and lists it in the user file.
func (e *testEnv) addProfile(extra map[string]any) {
	e.t.Helper()
	doc := map[string]any{"id": "example-team", "name": "Example team", "version": "2026.10.1"}
	for k, v := range extra {
		doc[k] = v
	}
	writeJSONFile(e.t, filepath.Join(e.home, "profile.json"), doc)
	e.writeUserConfig(map[string]any{"profiles": []any{map[string]any{"source": "file://${env:HOME}/profile.json"}}})
}

func (e *testEnv) writeUserConfig(doc map[string]any) {
	e.t.Helper()
	writeJSONFile(e.t, e.userConfig(), doc)
}

func (e *testEnv) readUserConfig() map[string]any {
	e.t.Helper()
	data, err := os.ReadFile(e.userConfig())
	if err != nil {
		e.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		e.t.Fatal(err)
	}
	return doc
}

// list decodes a response that is a JSON array of objects.
func list(t *testing.T, r response) []map[string]any {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal(r.body, &items); err != nil {
		t.Fatalf("not a JSON array: %v\n%s", err, r.body)
	}
	return items
}

// byKey finds the object whose key field equals value.
func byKey(items []map[string]any, key, value string) map[string]any {
	for _, it := range items {
		if it[key] == value {
			return it
		}
	}
	return nil
}

func (e *testEnv) repository(name string) map[string]any {
	e.t.Helper()
	r := e.get("/repositories")
	e.wantStatus(r, http.StatusOK)
	v := byKey(list(e.t, r), "name", name)
	if v == nil {
		e.t.Fatalf("repository %s is not listed: %s", name, r.body)
	}
	return v
}

func (e *testEnv) mutate(method, path string, body any, code int) response {
	e.t.Helper()
	r := e.request(method, path, body)
	e.wantStatus(r, code)
	return r
}

// createRepo adds a repository of the source and path "plugins" and returns the answer.
func (e *testEnv) createRepo(name, source string) map[string]any {
	e.t.Helper()
	body := map[string]any{"name": name, "source": "file://" + source, "path": "plugins"}
	return e.mutate(http.MethodPost, "/repositories", body, http.StatusCreated).json(e.t)
}

// pluginViews returns the plugins of the pending (or active) revision by ID.
func pluginViews(t *testing.T, repo map[string]any, revision string) map[string]map[string]any {
	t.Helper()
	items, _ := field(repo, revision, "plugins").([]any)
	out := map[string]map[string]any{}
	for _, it := range items {
		m, _ := it.(map[string]any)
		out[m["id"].(string)] = m
	}
	return out
}

func (e *testEnv) wantProblem(r response, code int, problemCode string) problem {
	e.t.Helper()
	e.wantStatus(r, code)
	p := r.problem(e.t)
	if p.Code != problemCode {
		e.t.Fatalf("problem code %q, want %q\n%s", p.Code, problemCode, r.body)
	}
	return p
}

// lastBroadcast returns the last PluginsChanged call and how many there were.
func (e *testEnv) lastBroadcast() (recordedPlugins, int) {
	e.bc.mu.Lock()
	defer e.bc.mu.Unlock()
	if len(e.bc.plugins) == 0 {
		return recordedPlugins{}, 0
	}
	return e.bc.plugins[len(e.bc.plugins)-1], len(e.bc.plugins)
}

func (e *testEnv) wantBroadcast(ids, repositories []string) {
	e.t.Helper()
	got, n := e.lastBroadcast()
	if n == 0 {
		e.t.Fatal("no PluginsChanged call")
	}
	if !sameStrings(got.ids, ids) || !sameStrings(got.repositories, repositories) {
		e.t.Errorf("PluginsChanged(%v, %v), want (%v, %v)", got.ids, got.repositories, ids, repositories)
	}
}

func sameStrings(a, b []string) bool { return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) }

func TestRepositoriesEmpty(t *testing.T) {
	e := newTestEnv(t)
	r := e.get("/repositories")
	e.wantStatus(r, http.StatusOK)
	if items := list(t, r); len(items) != 0 {
		t.Errorf("repositories = %v", items)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(r.body)), "[]") {
		t.Errorf("an empty list must be [], got %s", r.body)
	}

	tests := []struct {
		name, method, path string
		body               any
	}{
		{"sync", http.MethodPost, "/repositories/nope/sync", nil},
		{"approve", http.MethodPost, "/repositories/nope/approve", map[string]any{"revision": "abc"}},
		{"discard", http.MethodDelete, "/repositories/nope/pending", nil},
		{"delete", http.MethodDelete, "/repositories/nope", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			e.wantProblem(e.request(tt.method, tt.path, tt.body), http.StatusNotFound, codeNotFound)
		})
	}
	if _, n := e.lastBroadcast(); n != 0 {
		t.Errorf("%d broadcasts for unknown repositories", n)
	}
}

func TestRepositoryLifecycle(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	writeRepoPlugin(t, src, "gamma", "1.0.0")

	// create: the first revision waits for approval
	created := e.createRepo("team", src)
	if created["state"] != "pending" || created["kind"] != "plugins" || created["origin"] != "user" ||
		created["trust"] != "prompt" || created["enabled"] != true || created["locked"] != false {
		t.Fatalf("created = %v", created)
	}
	if created["source"] != "file://"+src || created["path"] != "plugins" || created["active"] != nil {
		t.Errorf("created = %v", created)
	}
	if field(created, "lastSync", "changed") != true || field(created, "lastSync", "at") == "" {
		t.Errorf("lastSync = %v", created["lastSync"])
	}
	e.wantBroadcast([]string{"alpha", "beta", "gamma"}, []string{"team"})
	revision, _ := field(created, "pending", "revision").(string)
	if revision == "" {
		t.Fatalf("no pending revision: %v", created)
	}
	pending := pluginViews(t, created, "pending")
	if len(pending) != 3 {
		t.Fatalf("pending plugins = %v", pending)
	}
	for id, p := range pending {
		if p["change"] != "added" || p["digest"] == "" {
			t.Errorf("%s = %v", id, p)
		}
	}
	perms, _ := pending["alpha"]["permissions"].([]any)
	if !reflect.DeepEqual(perms, []any{map[string]any{"kind": "network", "value": hostDownloads, "new": true}}) {
		t.Errorf("alpha permissions = %v", perms)
	}
	if pending["alpha"]["name"] != "Alpha" || pending["alpha"]["version"] != "1.0.0" {
		t.Errorf("alpha = %v", pending["alpha"])
	}

	// the user file got the entry
	entry := field(e.readUserConfig(), "repositories", "team")
	if !reflect.DeepEqual(entry, map[string]any{"kind": "plugins", "source": "file://" + src, "path": "plugins"}) {
		t.Errorf("user file entry = %v", entry)
	}

	// pending plugins are not loaded
	e.wantStatus(e.get("/plugins/alpha"), http.StatusNotFound)
	if byKey(list(t, e.get("/plugins")), "id", "alpha") != nil {
		t.Error("alpha is listed before the approval")
	}
	if got := e.repository("team"); got["state"] != "pending" {
		t.Errorf("listed = %v", got)
	}

	// approve with a wrong revision, then the right one
	e.wantProblem(e.request(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": "0000"}), http.StatusConflict, codeConflict)
	if _, n := e.lastBroadcast(); n != 1 {
		t.Errorf("a refused approval was broadcast (%d calls)", n)
	}
	for name, body := range map[string]any{"missing revision": map[string]any{}, "unknown field": map[string]any{"revision": revision, "x": 1}} {
		e.wantProblem(e.request(http.MethodPost, "/repositories/team/approve", body), http.StatusUnprocessableEntity, codeValidation)
		_ = name
	}
	approved := e.mutate(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": revision}, http.StatusOK).json(t)
	if approved["state"] != "active" || approved["pending"] != nil || field(approved, "active", "revision") != revision {
		t.Fatalf("approved = %v", approved)
	}
	e.wantBroadcast([]string{"alpha", "beta", "gamma"}, []string{"team"})
	for id, p := range pluginViews(t, approved, "active") {
		if p["change"] != nil {
			t.Errorf("active plugin %s has a change: %v", id, p)
		}
	}

	// the plugins are loaded now, with their origin
	r := e.get("/plugins")
	e.wantStatus(r, http.StatusOK)
	plugins := list(t, r)
	wantOrigin := map[string]any{"kind": "repository", "repository": "team", "revision": revision[:12]}
	for _, id := range []string{"alpha", "beta", "gamma"} {
		p := byKey(plugins, "id", id)
		if p == nil || !reflect.DeepEqual(p["origin"], wantOrigin) || p["state"] != "ready" {
			t.Errorf("plugin %s = %v", id, p)
		}
	}
	if p := byKey(plugins, "id", "demo"); p == nil || field(p, "origin", "kind") != "path" || field(p, "origin", "path") != e.plugins {
		t.Errorf("demo = %v", p)
	}
	if p := byKey(plugins, "id", "base"); p == nil || field(p, "origin", "kind") != "builtin" {
		t.Errorf("base = %v", p)
	}
	e.wantStatus(e.get("/plugins/alpha"), http.StatusOK)

	// the source changes: alpha gets a version and a permission, gamma is removed
	writeRepoPlugin(t, src, "alpha", "1.1.0", hostDownloads, hostCDN)
	if err := os.RemoveAll(filepath.Join(src, "plugins", "gamma")); err != nil {
		t.Fatal(err)
	}
	synced := e.mutate(http.MethodPost, "/repositories/team/sync", nil, http.StatusOK).json(t)
	if synced["state"] != "pending" || field(synced, "active", "revision") != revision || field(synced, "lastSync", "changed") != true {
		t.Fatalf("synced = %v", synced)
	}
	e.wantBroadcast([]string{"alpha", "beta", "gamma"}, []string{"team"})
	next, _ := field(synced, "pending", "revision").(string)
	if next == "" || next == revision {
		t.Fatalf("pending revision = %q", next)
	}
	pending = pluginViews(t, synced, "pending")
	if len(pending) != 3 {
		t.Fatalf("pending plugins = %v", pending)
	}
	alpha := pending["alpha"]
	if alpha["change"] != "changed" || alpha["previousVersion"] != "1.0.0" || alpha["version"] != "1.1.0" {
		t.Errorf("alpha = %v", alpha)
	}
	wantPerms := []any{
		map[string]any{"kind": "network", "value": hostDownloads, "new": false},
		map[string]any{"kind": "network", "value": hostCDN, "new": true},
	}
	if !reflect.DeepEqual(alpha["permissions"], wantPerms) {
		t.Errorf("alpha permissions = %v", alpha["permissions"])
	}
	if pending["beta"]["change"] != "unchanged" || pending["beta"]["previousVersion"] != nil {
		t.Errorf("beta = %v", pending["beta"])
	}
	if pending["gamma"]["change"] != "removed" {
		t.Errorf("gamma = %v", pending["gamma"])
	}
	// the active plugins are still the old ones
	if p := byKey(list(t, e.get("/plugins")), "id", "alpha"); p == nil || p["version"] != "1.0.0" {
		t.Errorf("alpha while pending = %v", p)
	}

	// syncing again changes nothing
	_, before := e.lastBroadcast()
	again := e.mutate(http.MethodPost, "/repositories/team/sync", nil, http.StatusOK).json(t)
	if again["state"] != "pending" || field(again, "pending", "revision") != next || field(again, "lastSync", "changed") != false {
		t.Errorf("synced again = %v", again)
	}
	if _, n := e.lastBroadcast(); n != before+1 {
		t.Errorf("a sync is announced once: %d calls, want %d", n, before+1)
	}

	// discard keeps the active revision
	discarded := e.mutate(http.MethodDelete, "/repositories/team/pending", nil, http.StatusOK).json(t)
	if discarded["state"] != "active" || discarded["pending"] != nil || field(discarded, "active", "revision") != revision {
		t.Errorf("discarded = %v", discarded)
	}
	e.wantBroadcast([]string{"alpha", "beta", "gamma"}, []string{"team"})
	e.wantProblem(e.request(http.MethodDelete, "/repositories/team/pending", nil), http.StatusNotFound, codeNotFound)
	e.wantProblem(e.request(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": next}), http.StatusConflict, codeConflict)
	if p := byKey(list(t, e.get("/plugins")), "id", "alpha"); p == nil || p["version"] != "1.0.0" {
		t.Errorf("alpha after the discard = %v", p)
	}

	// the same revision comes back as pending, and now the new one is approved
	synced = e.mutate(http.MethodPost, "/repositories/team/sync", nil, http.StatusOK).json(t)
	if field(synced, "pending", "revision") != next || field(synced, "lastSync", "changed") != true {
		t.Fatalf("synced = %v", synced)
	}
	e.mutate(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": next}, http.StatusOK)
	plugins = list(t, e.get("/plugins"))
	if p := byKey(plugins, "id", "alpha"); p == nil || p["version"] != "1.1.0" || field(p, "origin", "revision") != next[:12] {
		t.Errorf("alpha after the approval = %v", p)
	}
	if byKey(plugins, "id", "gamma") != nil {
		t.Error("gamma was removed from the repository but is still listed")
	}
	if got := e.repository("team"); got["state"] != "active" || field(got, "lastSync", "changed") != true {
		t.Errorf("listed = %v", got)
	}
}

func TestRepositoryCreateInvalid(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	fileSource := "file://" + src
	tests := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"bad name", map[string]any{"name": "Team!", "source": fileSource}, http.StatusUnprocessableEntity, codeValidation},
		{"name too short", map[string]any{"name": "a", "source": fileSource}, http.StatusUnprocessableEntity, codeValidation},
		{"missing source", map[string]any{"name": "team"}, http.StatusUnprocessableEntity, codeValidation},
		{"unknown field", map[string]any{"name": "team", "source": fileSource, "enabled": false}, http.StatusUnprocessableEntity, codeValidation},
		{"unknown trust", map[string]any{"name": "team", "source": fileSource, "trust": "always"}, http.StatusUnprocessableEntity, codeValidation},
		{"pinned without commit", map[string]any{"name": "team", "source": "git+file://" + src, "trust": "pinned", "ref": "main"}, http.StatusUnprocessableEntity, codeValidation},
		{"pinned without ref", map[string]any{"name": "team", "source": "git+https://git.example.com/team/plugins.git", "trust": "pinned"}, http.StatusUnprocessableEntity, codeValidation},
		{"pinned with file source", map[string]any{"name": "team", "source": fileSource, "trust": "pinned", "ref": strings.Repeat("a", 40)}, http.StatusUnprocessableEntity, codeValidation},
		{"ref with file source", map[string]any{"name": "team", "source": fileSource, "ref": "main"}, http.StatusUnprocessableEntity, codeValidation},
		{"ref that looks like an option", map[string]any{"name": "team", "source": "git+https://git.example.com/team/plugins.git", "ref": "--upload-pack=x"}, http.StatusUnprocessableEntity, codeValidation},
		{"path outside", map[string]any{"name": "team", "source": fileSource, "path": "../up"}, http.StatusUnprocessableEntity, codeValidation},
		{"absolute path", map[string]any{"name": "team", "source": fileSource, "path": "/etc"}, http.StatusUnprocessableEntity, codeValidation},
		{"source scheme", map[string]any{"name": "team", "source": "ftp://files.example.com/plugins"}, http.StatusUnprocessableEntity, codeValidation},
		{"source without scheme", map[string]any{"name": "team", "source": src}, http.StatusUnprocessableEntity, codeValidation},
		{"file source with a host", map[string]any{"name": "team", "source": "file://files.example.com/plugins"}, http.StatusUnprocessableEntity, codeValidation},
		{"relative file source", map[string]any{"name": "team", "source": "file://plugins"}, http.StatusUnprocessableEntity, codeValidation},
		{"not an object", `[1]`, http.StatusUnprocessableEntity, codeValidation},
		{"not JSON", `{`, http.StatusBadRequest, codeValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.wantProblem(e.request(http.MethodPost, "/repositories", tt.body), tt.status, tt.code)
			if _, err := os.Stat(e.userConfig()); err == nil {
				if doc := e.readUserConfig(); doc["repositories"] != nil {
					t.Errorf("the user file got an entry: %v", doc["repositories"])
				}
			}
			if _, err := os.Stat(e.repoDir("team")); err == nil {
				t.Error("a directory was created")
			}
		})
	}
	if got := list(t, e.get("/repositories")); len(got) != 0 {
		t.Errorf("repositories = %v", got)
	}
	if _, n := e.lastBroadcast(); n != 0 {
		t.Errorf("%d broadcasts for refused requests", n)
	}
}

func TestRepositoryNameConflict(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	e.createRepo("team", src)
	_, before := e.lastBroadcast()

	p := e.wantProblem(e.request(http.MethodPost, "/repositories", map[string]any{"name": "team", "source": "file://" + src}), http.StatusConflict, codeConflict)
	if !strings.Contains(p.Detail, "exists") {
		t.Errorf("detail = %q", p.Detail)
	}
	if _, n := e.lastBroadcast(); n != before {
		t.Error("the refused request was broadcast")
	}
	// the entry of the first request is untouched
	if entry := field(e.readUserConfig(), "repositories", "team"); !reflect.DeepEqual(entry, map[string]any{"kind": "plugins", "source": "file://" + src, "path": "plugins"}) {
		t.Errorf("entry = %v", entry)
	}

	// system and profile entries block the name, too
	e.writeSystemConfig(map[string]any{"repositories": map[string]any{
		"corp": map[string]any{"kind": "plugins", "source": "file://" + src},
	}})
	e.wantProblem(e.request(http.MethodPost, "/repositories", map[string]any{"name": "corp", "source": "file://" + src}), http.StatusConflict, codeConflict)
}

func TestDeleteRepository(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	created := e.createRepo("team", src)
	revision := field(created, "pending", "revision").(string)
	e.mutate(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": revision}, http.StatusOK)
	e.createRepo("other", src)
	if _, err := os.Stat(e.repoDir("team")); err != nil {
		t.Fatalf("no repository directory: %v", err)
	}

	r := e.mutate(http.MethodDelete, "/repositories/team", nil, http.StatusNoContent)
	if len(r.body) != 0 {
		t.Errorf("body = %q", r.body)
	}
	e.wantBroadcast([]string{"alpha", "beta"}, []string{"team"})
	if _, err := os.Stat(e.repoDir("team")); !os.IsNotExist(err) {
		t.Errorf("the repository directory is still there: %v", err)
	}
	repos, _ := e.readUserConfig()["repositories"].(map[string]any)
	if _, ok := repos["team"]; ok || repos["other"] == nil {
		t.Errorf("user file repositories = %v", repos)
	}
	if _, err := os.Stat(e.repoDir("other")); err != nil {
		t.Errorf("the other repository is gone: %v", err)
	}
	if got := list(t, e.get("/repositories")); len(got) != 1 || got[0]["name"] != "other" {
		t.Errorf("repositories = %v", got)
	}
	e.wantStatus(e.get("/plugins/alpha"), http.StatusNotFound)
	e.wantProblem(e.request(http.MethodDelete, "/repositories/team", nil), http.StatusNotFound, codeNotFound)

	// the last entry takes the repositories key with it
	e.mutate(http.MethodDelete, "/repositories/other", nil, http.StatusNoContent)
	if doc := e.readUserConfig(); doc["repositories"] != nil {
		t.Errorf("user file = %v", doc)
	}
}

func TestRepositoryLayers(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	entry := func(extra map[string]any) map[string]any {
		m := map[string]any{"kind": "plugins", "source": "file://" + src, "path": "plugins"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	e.writeSystemConfig(map[string]any{"repositories": map[string]any{"corp": entry(nil)}})
	e.addProfile(map[string]any{"repositories": map[string]any{
		"locked-one": entry(map[string]any{"locked": true}),
		"open-one":   entry(nil),
	}})
	// the user file lists the profile and tries to replace two entries of it
	doc := e.readUserConfig()
	doc["repositories"] = map[string]any{
		"locked-one": entry(map[string]any{"source": "file://" + src + "/user-copy"}),
		"open-one":   entry(map[string]any{"trust": "prompt", "path": "plugins"}),
		"mine":       entry(nil),
	}
	e.writeUserConfig(doc)

	items := list(t, e.get("/repositories"))
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it["name"].(string))
	}
	if want := []string{"corp", "locked-one", "mine", "open-one"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for _, c := range []struct {
		name, origin, source string
		locked               bool
	}{
		{"corp", "system", "file://" + src, false},
		{"locked-one", "profile:example-team", "file://" + src, true},
		{"mine", "user", "file://" + src, false},
		{"open-one", "user", "file://" + src, false},
	} {
		v := byKey(items, "name", c.name)
		if v["origin"] != c.origin || v["locked"] != c.locked || v["source"] != c.source {
			t.Errorf("%s = %v", c.name, v)
		}
	}

	// only entries of the user file can be removed
	for _, name := range []string{"corp", "locked-one"} {
		e.wantProblem(e.request(http.MethodDelete, "/repositories/"+name, nil), http.StatusConflict, codeConflict)
	}
	if _, n := e.lastBroadcast(); n != 0 {
		t.Errorf("%d broadcasts for refused removals", n)
	}
	repos, _ := e.readUserConfig()["repositories"].(map[string]any)
	if repos["locked-one"] == nil {
		t.Error("the user file entry that the lock hides was removed")
	}

	// system and profile entries can be synced and approved like others
	synced := e.mutate(http.MethodPost, "/repositories/corp/sync", nil, http.StatusOK).json(t)
	if synced["state"] != "pending" || synced["origin"] != "system" {
		t.Fatalf("synced = %v", synced)
	}
	e.mutate(http.MethodPost, "/repositories/corp/approve", map[string]any{"revision": field(synced, "pending", "revision")}, http.StatusOK)
	if p := byKey(list(t, e.get("/plugins")), "id", "alpha"); p == nil || field(p, "origin", "repository") != "corp" {
		t.Errorf("alpha = %v", p)
	}

	// a user entry of an open profile entry replaces it
	e.mutate(http.MethodDelete, "/repositories/open-one", nil, http.StatusNoContent)
	if v := e.repository("open-one"); v["origin"] != "profile:example-team" {
		t.Errorf("open-one after the user entry is gone = %v", v)
	}
}

func TestRepositoryDisabledAndUnsupported(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	created := e.createRepo("team", src)
	revision := field(created, "pending", "revision").(string)
	e.mutate(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": revision}, http.StatusOK)
	e.wantStatus(e.get("/plugins/alpha"), http.StatusOK)

	doc := e.readUserConfig()
	repos := doc["repositories"].(map[string]any)
	repos["team"].(map[string]any)["enabled"] = false
	repos["catalog-one"] = map[string]any{"kind": "catalog", "source": "https://catalog.example.com/index.json"}
	e.writeUserConfig(doc)

	team := e.repository("team")
	if team["state"] != "disabled" || team["enabled"] != false || field(team, "active", "revision") != revision {
		t.Errorf("team = %v", team)
	}
	e.wantStatus(e.get("/plugins/alpha"), http.StatusNotFound)
	if byKey(list(t, e.get("/plugins")), "id", "beta") != nil {
		t.Error("beta of a switched-off repository is listed")
	}
	e.wantProblem(e.request(http.MethodPost, "/repositories/team/sync", nil), http.StatusConflict, codeConflict)

	cat := e.repository("catalog-one")
	if cat["state"] != "unsupported" || cat["kind"] != "catalog" || cat["trust"] != nil || cat["active"] != nil {
		t.Errorf("catalog-one = %v", cat)
	}
	e.wantProblem(e.request(http.MethodPost, "/repositories/catalog-one/sync", nil), http.StatusConflict, codeConflict)
	e.wantProblem(e.request(http.MethodPost, "/repositories/catalog-one/approve", map[string]any{"revision": "abc"}), http.StatusConflict, codeConflict)

	// switching it on again loads the active revision
	repos["team"].(map[string]any)["enabled"] = true
	e.writeUserConfig(doc)
	if v := e.repository("team"); v["state"] != "active" {
		t.Errorf("team = %v", v)
	}
	e.wantStatus(e.get("/plugins/alpha"), http.StatusOK)
}

func TestRepositorySyncFailure(t *testing.T) {
	e := newTestEnv(t)
	src := repoSource(t)
	missing := filepath.Join(t.TempDir(), "gone")

	// the entry stays when the first sync fails
	body := map[string]any{"name": "team", "source": "file://" + missing}
	created := e.mutate(http.MethodPost, "/repositories", body, http.StatusCreated).json(t)
	if created["state"] != "error" || created["lastError"] == "" || created["lastError"] == nil || created["active"] != nil || created["pending"] != nil {
		t.Fatalf("created = %v", created)
	}
	if got := e.repository("team"); got["state"] != "error" || got["lastError"] != created["lastError"] {
		t.Errorf("listed = %v", got)
	}
	e.wantBroadcast([]string{}, []string{"team"})

	// a sync of the missing source answers with a problem and keeps the error
	r := e.request(http.MethodPost, "/repositories/team/sync", nil)
	if r.code < 400 {
		t.Fatalf("status %d\n%s", r.code, r.body)
	}
	if p := r.problem(t); p.Code == "" {
		t.Errorf("problem = %+v", p)
	}
	if got := e.repository("team"); got["state"] != "error" || got["lastError"] == "" {
		t.Errorf("after the sync = %v", got)
	}

	// the source appears: the next sync clears the error
	if err := os.Rename(src, missing); err != nil {
		t.Fatal(err)
	}
	synced := e.mutate(http.MethodPost, "/repositories/team/sync", nil, http.StatusOK).json(t)
	if synced["state"] != "new" && synced["state"] != "pending" {
		t.Errorf("synced = %v", synced)
	}
	if synced["lastError"] != nil {
		t.Errorf("lastError = %v", synced["lastError"])
	}

	// a path that does not exist in the source is an error, too
	other := map[string]any{"name": "wrong-path", "source": "file://" + missing, "path": "nothing/here"}
	got := e.mutate(http.MethodPost, "/repositories", other, http.StatusCreated).json(t)
	if got["state"] != "error" || !strings.Contains(got["lastError"].(string), "nothing/here") {
		t.Errorf("wrong-path = %v", got)
	}
}

// gitIn runs git in dir with a fixed identity and no configuration of the user.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.CommandContext(context.Background(), "git", full...) //nolint:gosec // test helper, the arguments are literals of the tests
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRepositoryGitSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	e := newTestEnv(t)
	src := repoSource(t)
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "add", "-A")
	gitIn(t, src, "commit", "-q", "-m", "first")
	commit := gitIn(t, src, "rev-parse", "HEAD")
	if len(commit) != 40 {
		t.Fatalf("commit = %q", commit)
	}

	t.Run("prompt", func(t *testing.T) {
		body := map[string]any{"name": "gitted", "source": "git+file://" + src, "path": "plugins"}
		created := e.mutate(http.MethodPost, "/repositories", body, http.StatusCreated).json(t)
		if created["state"] != "pending" || field(created, "pending", "revision") != commit {
			t.Fatalf("created = %v", created)
		}
		e.mutate(http.MethodPost, "/repositories/gitted/approve", map[string]any{"revision": commit}, http.StatusOK)
		p := byKey(list(t, e.get("/plugins")), "id", "alpha")
		if p == nil || field(p, "origin", "repository") != "gitted" || field(p, "origin", "revision") != commit[:12] {
			t.Errorf("alpha = %v", p)
		}
	})

	t.Run("pinned", func(t *testing.T) {
		body := map[string]any{"name": "pinned-one", "source": "git+file://" + src, "path": "plugins", "ref": commit, "trust": "pinned"}
		created := e.mutate(http.MethodPost, "/repositories", body, http.StatusCreated).json(t)
		if created["state"] != "active" || created["pending"] != nil || field(created, "active", "revision") != commit || created["trust"] != "pinned" {
			t.Fatalf("created = %v", created)
		}
		e.wantBroadcast([]string{"alpha", "beta"}, []string{"pinned-one"})
		if p := byKey(list(t, e.get("/plugins")), "id", "beta"); p == nil {
			t.Error("beta is not loaded")
		}
		e.wantProblem(e.request(http.MethodPost, "/repositories/pinned-one/approve", map[string]any{"revision": commit}), http.StatusConflict, codeConflict)
		synced := e.mutate(http.MethodPost, "/repositories/pinned-one/sync", nil, http.StatusOK).json(t)
		if synced["state"] != "active" || field(synced, "lastSync", "changed") != false {
			t.Errorf("synced = %v", synced)
		}
	})
}

func TestPatchPlugin(t *testing.T) {
	e := newTestEnv(t)
	write(t, filepath.Join(e.plugins, "kernel", "plugin.json"), repoManifest("kernel", "1.0.0", "core"), 0o600)

	state := func(id string) any {
		r := e.get("/plugins/" + id)
		e.wantStatus(r, http.StatusOK)
		return r.json(t)["state"]
	}
	if state("demo") != "ready" {
		t.Fatalf("demo = %v", state("demo"))
	}

	off := e.mutate(http.MethodPatch, "/plugins/demo", map[string]any{"enabled": false}, http.StatusOK).json(t)
	if off["id"] != "demo" || off["state"] != "disabled" {
		t.Errorf("off = %v", off)
	}
	e.wantBroadcast([]string{"demo"}, nil)
	if disabled := field(e.readUserConfig(), "plugins", "disabled"); !reflect.DeepEqual(disabled, []any{"demo"}) {
		t.Errorf("plugins.disabled = %v", disabled)
	}
	if state("demo") != "disabled" {
		t.Errorf("demo = %v", state("demo"))
	}

	// the same again changes nothing and announces nothing
	_, before := e.lastBroadcast()
	again := e.mutate(http.MethodPatch, "/plugins/demo", map[string]any{"enabled": false}, http.StatusOK).json(t)
	if again["state"] != "disabled" {
		t.Errorf("again = %v", again)
	}
	if _, n := e.lastBroadcast(); n != before {
		t.Errorf("an unchanged switch was announced (%d calls, want %d)", n, before)
	}

	on := e.mutate(http.MethodPatch, "/plugins/demo", map[string]any{"enabled": true}, http.StatusOK).json(t)
	if on["state"] != "ready" {
		t.Errorf("on = %v", on)
	}
	e.wantBroadcast([]string{"demo"}, nil)
	if doc := e.readUserConfig(); doc["plugins"] != nil {
		t.Errorf("user file = %v", doc)
	}
	_, before = e.lastBroadcast()
	e.mutate(http.MethodPatch, "/plugins/demo", map[string]any{"enabled": true}, http.StatusOK)
	if _, n := e.lastBroadcast(); n != before {
		t.Error("an unchanged switch was announced")
	}

	// plugins of a repository can be switched like others
	src := repoSource(t)
	created := e.createRepo("team", src)
	e.mutate(http.MethodPost, "/repositories/team/approve", map[string]any{"revision": field(created, "pending", "revision")}, http.StatusOK)
	if p := e.mutate(http.MethodPatch, "/plugins/alpha", map[string]any{"enabled": false}, http.StatusOK).json(t); p["state"] != "disabled" || field(p, "origin", "repository") != "team" {
		t.Errorf("alpha = %v", p)
	}

	t.Run("core plugin", func(t *testing.T) {
		if state("kernel") != "ready" {
			t.Fatalf("kernel = %v", state("kernel"))
		}
		p := e.wantProblem(e.request(http.MethodPatch, "/plugins/kernel", map[string]any{"enabled": false}), http.StatusConflict, codeConflict)
		if !strings.Contains(p.Detail, "core") {
			t.Errorf("detail = %q", p.Detail)
		}
		e.mutate(http.MethodPatch, "/plugins/kernel", map[string]any{"enabled": true}, http.StatusOK)
	})

	t.Run("switched off by the system file", func(t *testing.T) {
		p := e.wantProblem(e.request(http.MethodPatch, "/plugins/base", map[string]any{"enabled": true}), http.StatusConflict, codeConflict)
		if !strings.Contains(p.Detail, "system") {
			t.Errorf("detail = %q", p.Detail)
		}
		// off stays possible and leaves the system's choice alone
		e.mutate(http.MethodPatch, "/plugins/base", map[string]any{"enabled": false}, http.StatusOK)
	})

	t.Run("switched off by a profile", func(t *testing.T) {
		e.addProfile(map[string]any{"plugins": map[string]any{"disable": []string{"gap"}}})
		p := e.wantProblem(e.request(http.MethodPatch, "/plugins/gap", map[string]any{"enabled": true}), http.StatusConflict, codeConflict)
		if !strings.Contains(p.Detail, "profile") {
			t.Errorf("detail = %q", p.Detail)
		}
	})

	t.Run("unknown plugin", func(_ *testing.T) {
		e.wantProblem(e.request(http.MethodPatch, "/plugins/nope", map[string]any{"enabled": true}), http.StatusNotFound, codeNotFound)
	})

	t.Run("invalid body", func(_ *testing.T) {
		for _, body := range []any{map[string]any{}, map[string]any{"enabled": "yes"}, map[string]any{"enabled": true, "x": 1}, `[true]`} {
			e.wantProblem(e.request(http.MethodPatch, "/plugins/demo", body), http.StatusUnprocessableEntity, codeValidation)
		}
		e.wantProblem(e.request(http.MethodPatch, "/plugins/demo", `{`), http.StatusBadRequest, codeValidation)
	})
}
