package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

const manifest = `{
  "id": "demo",
  "version": "1.0.0",
  "name": "Demo",
  "settings": {
    "defaultVersion": { "type": "integer", "default": 21, "label": "Default version" }
  },
  "checks": {
    "state": { "label": "State", "run": ["check.sh"], "cache": "5m", "refreshOn": ["session.finished:demo.*"] }
  },
  "actions": {
    "install": {
      "label": "Install",
      "run": ["act.sh"],
      "timeout": "10m",
      "locks": ["tool"],
      "params": {
        "version": { "type": "integer", "default": "${demo.defaultVersion}", "label": "Version" },
        "channel": { "type": "string", "enum": ["lts", "current"], "default": "lts", "label": "Channel" },
        "target": { "type": "string", "label": "Target" },
        "broken": { "type": "string", "default": "${demo.nothing}", "label": "Broken" }
      }
    }
  }
}`

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T) (*State, string) {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{
		"HOME":                home,
		"PATH":                os.Getenv("PATH"),
		"XDG_RUNTIME_DIR":     filepath.Join(home, "run"),
		SystemConfigEnv:       filepath.Join(home, "none.json"),
		"XDG_CURRENT_DESKTOP": "GNOME:ubuntu",
	}
	dir := filepath.Join(home, ".local", "share", "devbox", "plugins", "demo")
	write(t, filepath.Join(dir, "plugin.json"), manifest, 0o600)
	write(t, filepath.Join(dir, "check.sh"), "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"warn\",\"summary\":\"careful\",\"details\":[{\"label\":\"A\",\"value\":\"1\"}],\"hints\":[{\"text\":\"Do it\",\"action\":\"demo.install\"}],\"data\":{\"n\":1}}'\n", 0o700)
	write(t, filepath.Join(dir, "act.sh"), "#!/usr/bin/env bash\necho out\nprintf '%s\\n' '{\"type\":\"result\",\"summary\":\"ok\"}' >&3\n", 0o700)
	write(t, filepath.Join(dir, "i18n", "de.json"), `{"$schema":"x","demo.title":"Demo"}`, 0o600)
	write(t, filepath.Join(dir, "i18n", "bad.json"), `{"Title":"x"}`, 0o600)
	write(t, filepath.Join(home, "none.json"), `{"plugins":{"disabled":["base","credentials","java","maven","node","npm","registry","system"]}}`, 0o600)
	st, err := Load(Env{Getenv: func(k string) string { return env[k] }, Environ: func() []string { return nil }}, Build{Version: "1.0.0"}, true)
	if err != nil {
		t.Fatal(err)
	}
	return st, home
}

func TestResolveParams(t *testing.T) {
	st, _ := load(t)
	action := map[string]any{"params": map[string]any{
		"version": map[string]any{"type": "integer", "default": "${demo.defaultVersion}"},
		"channel": map[string]any{"type": "string", "enum": []any{"lts", "current"}, "default": "lts"},
		"target":  map[string]any{"type": "string"},
	}}
	tests := []struct {
		name    string
		values  map[string]any
		wantKey string
		want    map[string]any
	}{
		{"defaults and required", map[string]any{"target": "x"}, "", map[string]any{"version": json.Number("21"), "channel": "lts", "target": "x"}},
		{"given wins", map[string]any{"target": "x", "version": json.Number("17")}, "", map[string]any{"version": json.Number("17"), "channel": "lts", "target": "x"}},
		{"required missing", nil, "demo.install#target", nil},
		{"unknown", map[string]any{"target": "x", "colour": "red"}, "demo.install#colour", nil},
		{"wrong type", map[string]any{"target": "x", "version": "many"}, "demo.install#version", nil},
		{"outside enum", map[string]any{"target": "x", "channel": "nightly"}, "demo.install#channel", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.ResolveParams(action, "demo.install", tt.values)
			if tt.wantKey != "" {
				var ce *config.Error
				if err == nil || !asConfigError(err, &ce) || ce.Key != tt.wantKey || ce.Code != config.CodeValidation {
					t.Fatalf("err = %v, want a validation error for %s", err, tt.wantKey)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func asConfigError(err error, target **config.Error) bool {
	ce, ok := err.(*config.Error) //nolint:errorlint // the test wants the top-level type
	if ok {
		*target = ce
	}
	return ok
}

func TestResolvedDefaults(t *testing.T) {
	st, _ := load(t)
	d := st.ResolvedDefaults(st.Manifests[0])
	install := d["install"]
	if install["version"] != json.Number("21") || install["channel"] != "lts" {
		t.Errorf("defaults = %v", d)
	}
	// no default, or a default that cannot be resolved: left out
	if _, has := install["target"]; has {
		t.Error("target has no default")
	}
	if _, has := install["broken"]; has {
		t.Error("a default that cannot be resolved must be left out")
	}
}

func TestActionSpec(t *testing.T) {
	st, _ := load(t)
	p := FindPlugin(st.Evaluate(), "demo")
	spec, title, err := st.ActionSpec(p, "install", map[string]any{"version": 1}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Install" || spec.Kind != runner.KindAction || spec.PluginID != "demo" || !spec.DryRun || !spec.Yes {
		t.Errorf("spec = %+v", spec)
	}
	if spec.Timeout.Minutes() != 10 || len(spec.Locks) != 1 || spec.Locks[0] != "tool" || !history.ValidID(spec.SessionID) {
		t.Errorf("spec = %+v", spec)
	}
	if spec.Host["desktop"] != "gnome" {
		t.Errorf("host = %v", spec.Host)
	}
	if _, _, err := st.ActionSpec(p, "nothing", nil, false, false); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("unknown action: %v", err)
	}
}

func TestSettings(t *testing.T) {
	st, _ := load(t)
	var items []SettingItem
	for _, it := range Settings(st) {
		if it.Plugin == "demo" {
			items = append(items, it)
		}
	}
	if len(items) != 1 || items[0].Key != "demo.defaultVersion" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Value != json.Number("21") || items[0].Origin != config.LayerPlugin || items[0].Locked {
		t.Errorf("default = %+v", items[0])
	}
	// neither a value nor a default: value and origin are absent from the JSON
	out, err := json.Marshal(SettingItem{Key: "demo.token", Plugin: "demo", Field: map[string]any{}})
	if err != nil || strings.Contains(string(out), `"value"`) || strings.Contains(string(out), `"origin"`) || strings.Contains(string(out), "lockedBy") {
		t.Errorf("value, origin and lockedBy must be absent: %s", out)
	}
}

func TestMessages(t *testing.T) {
	st, _ := load(t)
	m := st.Manifests[0]
	got, err := st.Messages(m, "de")
	if err != nil || len(got) != 1 || got["demo.title"] != "Demo" {
		t.Errorf("messages = %v, %v", got, err)
	}
	if _, err := st.Messages(m, "fr"); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("missing language: %v", err)
	}
	if _, err := st.Messages(m, "../plugin"); !config.IsCode(err, config.CodeValidation) {
		t.Errorf("path in language: %v", err)
	}
	if _, err := st.Messages(m, "bad"); err == nil {
		t.Error("a file that does not follow messages.v1 must be refused")
	}
}

func TestRunChecks(t *testing.T) {
	st, _ := load(t)
	r, err := st.NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	var notified []string
	out := RunChecksNotify(context.Background(), st, r, nil, func(o CheckOutcome) { notified = append(notified, o.ID) })
	if len(out) != 1 || len(notified) != 1 {
		t.Fatalf("outcomes = %+v", out)
	}
	o := out[0]
	if o.ID != "demo.state" || o.Status != "warn" || o.Summary != "careful" || o.Label != "State" {
		t.Errorf("outcome = %+v", o)
	}
	if len(o.Details) != 1 || o.Details[0].Value != "1" || len(o.Hints) != 1 || o.Hints[0].Action != "demo.install" || o.Data["n"] == nil {
		t.Errorf("details, hints or data: %+v", o)
	}
	if o.Cache.Minutes() != 5 || len(o.RefreshOn) != 1 {
		t.Errorf("cache and refreshOn: %+v", o)
	}
	if got := RunChecks(context.Background(), st, r, func(string) bool { return false }); len(got) != 0 {
		t.Errorf("the filter was ignored: %+v", got)
	}
}

func TestRunSessionOrderAndSettings(t *testing.T) {
	st, _ := load(t)
	r, err := st.NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	p := FindPlugin(st.Evaluate(), "demo")
	spec, title, err := st.ActionSpec(p, "install", map[string]any{"version": json.Number("1"), "channel": "lts", "target": "x", "broken": "y"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	var warnings []string
	res, err := RunSession(context.Background(), st, r, spec, "demo.install", title, history.OriginAPI, Sink{
		OnOutput: func(seq int, _ []byte) { seqs = append(seqs, seq) },
		Event:    func(ev history.Event) { seqs = append(seqs, ev.Seq) },
		Warn:     func(w string) { warnings = append(warnings, w) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != runner.StateSuccess {
		t.Fatalf("result = %+v (%v)", res, warnings)
	}
	for i, s := range seqs {
		if s != i {
			t.Fatalf("sequence numbers %v are not consecutive", seqs)
		}
	}
	if len(seqs) < 2 {
		t.Errorf("seqs = %v", seqs)
	}
	store := history.New(r.Env.StateDir, st.Validator)
	e, ok, err := store.Get(spec.SessionID)
	if err != nil || !ok || e.Origin != history.OriginAPI || e.Summary != "ok" || e.Title != "Install" {
		t.Errorf("entry = %+v %v %v", e, ok, err)
	}
}

func TestServerDefaults(t *testing.T) {
	st, _ := load(t)
	sc, err := st.Server()
	if err != nil {
		t.Fatal(err)
	}
	want, err := schema.DefaultString("config", "/properties/server/properties/listen/default")
	if err != nil || sc.Listen != want || sc.Auth != "local" {
		t.Errorf("server = %+v, schema default %q (%v)", sc, want, err)
	}
	if _, err := schema.Default("config", "/properties/server/properties/nothing/default"); err == nil {
		t.Error("a pointer outside the schema must be an error")
	}
}

func TestCredentialsEmpty(t *testing.T) {
	st, _ := load(t)
	got, err := Credentials(st)
	if err != nil || len(got) != 0 {
		t.Errorf("credentials = %v, %v", got, err)
	}
}
