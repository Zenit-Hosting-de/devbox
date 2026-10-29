package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// loadAt loads the state of a home directory prepared by the caller.
func loadAt(t *testing.T, home string) *State {
	t.Helper()
	env := map[string]string{
		"HOME":          home,
		"PATH":          os.Getenv("PATH"),
		SystemConfigEnv: filepath.Join(home, "system.json"),
	}
	st, err := Load(Env{Getenv: func(k string) string { return env[k] }, Environ: func() []string { return nil }}, Build{Version: "1.0.0"}, true)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func pluginManifest(id, category string) string {
	return `{"id":"` + id + `","version":"1.0.0","name":"` + id + `","category":"` + category + `"}`
}

func TestRepositoryPluginsAndOrigin(t *testing.T) {
	home := t.TempDir()
	// the same ID in plugins.paths, in a repository and bundled: the path wins, then the repository
	write(t, filepath.Join(home, "paths", "shared", "plugin.json"), pluginManifest("shared", "tool"), 0o600)
	write(t, filepath.Join(home, "src", "shared", "plugin.json"), pluginManifest("shared", "tool"), 0o600)
	write(t, filepath.Join(home, "src", "base", "plugin.json"), pluginManifest("base", "tool"), 0o600)
	write(t, filepath.Join(home, "src", "only", "plugin.json"), pluginManifest("only", "tool"), 0o600)
	write(t, filepath.Join(home, ".config", "devbox", "config.json"), `{
  "plugins": { "paths": ["${env:HOME}/paths"] },
  "repositories": { "team": { "kind": "plugins", "source": "file://${env:HOME}/src" } }
}`, 0o600)

	st := loadAt(t, home)
	ctx := context.Background()
	if p := FindPlugin(st.Evaluate(), "only"); p != nil {
		t.Fatal("a repository without an active revision must not provide plugins")
	}
	if err := st.SyncRepository(ctx, "team"); err != nil {
		t.Fatal(err)
	}
	st = loadAt(t, home)
	if p := FindPlugin(st.Evaluate(), "only"); p != nil {
		t.Fatal("a pending revision must not provide plugins")
	}
	view, err := st.RepositoryView("team")
	if err != nil || view.Pending == nil {
		t.Fatalf("view = %+v, %v", view, err)
	}
	if err := st.ApproveRepository(ctx, "team", view.Pending.Revision); err != nil {
		t.Fatal(err)
	}
	st = loadAt(t, home)
	short := view.Pending.Revision[:12]
	plugins := st.Evaluate()
	wants := map[string]plugin.Origin{
		"only":   {Kind: plugin.OriginRepository, Repository: "team", Revision: short},
		"shared": {Kind: plugin.OriginPath, Path: filepath.Join(home, "paths")},
		"base":   {Kind: plugin.OriginRepository, Repository: "team", Revision: short},
	}
	for id, want := range wants {
		p := FindPlugin(plugins, id)
		if p == nil || p.Origin != want {
			t.Errorf("%s: origin = %+v, want %+v", id, p, want)
		}
	}
	if p := FindPlugin(plugins, "base"); p != nil && len(p.Shadowed) != 1 {
		t.Errorf("base replaces the bundled plugin and notes it: %v", p.Shadowed)
	}
	if p := FindPlugin(plugins, "java"); p == nil || p.Origin.Kind != plugin.OriginBuiltin {
		t.Errorf("java = %+v", p)
	}

	// a disabled repository provides nothing
	write(t, filepath.Join(home, ".config", "devbox", "config.json"), `{
  "repositories": { "team": { "kind": "plugins", "source": "file://${env:HOME}/src", "enabled": false } }
}`, 0o600)
	st = loadAt(t, home)
	if p := FindPlugin(st.Evaluate(), "only"); p != nil {
		t.Error("a disabled repository must not provide plugins")
	}
	if v, _ := st.RepositoryView("team"); v.State != "disabled" {
		t.Errorf("state = %s", v.State)
	}
}

func TestRemoveRepositoryOrigins(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "system.json"), `{"repositories":{"from-system":{"kind":"plugins","source":"file:///srv/a"}}}`, 0o600)
	write(t, filepath.Join(home, "profile.json"), `{"id":"team","name":"Team","version":"1","repositories":{"from-profile":{"kind":"plugins","source":"file:///srv/b"}}}`, 0o600)
	write(t, filepath.Join(home, ".config", "devbox", "config.json"), `{
  "profiles": [{ "source": "file://${env:HOME}", "path": "profile.json" }],
  "repositories": { "mine": { "kind": "plugins", "source": "file:///srv/c" } }
}`, 0o600)
	st := loadAt(t, home)
	ctx := context.Background()
	for _, name := range []string{"from-system", "from-profile"} {
		if err := st.RemoveRepository(ctx, name); !config.IsCode(err, config.CodeConflict) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := st.RemoveRepository(ctx, "nope"); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if err := st.RemoveRepository(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "devbox", "config.json"))
	if err != nil || strings.Contains(string(data), "repositories") {
		t.Errorf("config = %s, %v", data, err)
	}
	st = loadAt(t, home)
	names := ""
	for _, e := range st.Repositories {
		names += e.Name + ":" + e.Origin + " "
	}
	if names != "from-profile:profile:team from-system:system " {
		t.Errorf("entries = %s", names)
	}
}

func TestSetPluginEnabled(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "devbox", "plugins")
	for id, category := range map[string]string{"tool-a": "tool", "tool-b": "tool", "tool-c": "tool", "core-x": "core"} {
		write(t, filepath.Join(dir, id, "plugin.json"), pluginManifest(id, category), 0o600)
	}
	write(t, filepath.Join(home, "system.json"), `{"plugins":{"disabled":["tool-b"]}}`, 0o600)
	write(t, filepath.Join(home, "profile.json"), `{"id":"team","name":"Team","version":"1","plugins":{"disable":["tool-c"]}}`, 0o600)
	cfg := filepath.Join(home, ".config", "devbox", "config.json")
	write(t, cfg, `{"profiles":[{"source":"file://${env:HOME}","path":"profile.json"}]}`, 0o600)

	state := func(st *State, id string) plugin.State {
		return FindPlugin(st.Evaluate(), id).State
	}
	steps := []struct {
		id      string
		enabled bool
		changed bool
		code    string
		want    plugin.State
	}{
		{"tool-a", false, true, "", plugin.StateDisabled},
		{"tool-a", false, false, "", plugin.StateDisabled}, // twice: nothing changes
		{"tool-a", true, true, "", plugin.StateReady},
		{"tool-a", true, false, "", plugin.StateReady},
		{"core-x", false, false, config.CodeConflict, plugin.StateReady},
		{"tool-b", true, false, config.CodeConflict, plugin.StateDisabled}, // system file
		{"tool-c", true, false, config.CodeConflict, plugin.StateDisabled}, // profile
		{"nope", false, false, config.CodeNotFound, ""},
	}
	for _, s := range steps {
		changed, err := loadAt(t, home).SetPluginEnabled(s.id, s.enabled)
		switch {
		case s.code != "" && !config.IsCode(err, s.code):
			t.Errorf("%s enabled=%v: error = %v, want code %s", s.id, s.enabled, err, s.code)
			continue
		case s.code == "" && err != nil:
			t.Errorf("%s enabled=%v: %v", s.id, s.enabled, err)
			continue
		}
		if changed != s.changed {
			t.Errorf("%s enabled=%v: changed = %v", s.id, s.enabled, changed)
		}
		if s.want != "" {
			if got := state(loadAt(t, home), s.id); got != s.want {
				t.Errorf("%s enabled=%v: state = %s, want %s", s.id, s.enabled, got, s.want)
			}
		}
	}
	// the profile's reason is named in the error
	_, err := loadAt(t, home).SetPluginEnabled("tool-c", true)
	if err == nil || !strings.Contains(err.Error(), "profile:team") {
		t.Errorf("error = %v", err)
	}
	// switching tool-a on and off again leaves the file without plugins
	data, _ := os.ReadFile(cfg)
	if strings.Contains(string(data), `"plugins"`) {
		t.Errorf("config = %s", data)
	}
}
