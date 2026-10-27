package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func writePlugin(t *testing.T, root, dir, manifest string) {
	t.Helper()
	p := filepath.Join(root, dir)
	if err := os.MkdirAll(p, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, ManifestFile), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func manifest(id, extra string) string {
	return `{"id": "` + id + `", "version": "1.2.0", "name": "` + id + `"` + extra + `}`
}

func testEnv() Env {
	return Env{
		Version: "1.0.0", OS: "linux", Arch: "amd64", Distro: "debian",
		LookPath: func(name string) (string, error) {
			if name == "tar" {
				return "/usr/bin/tar", nil
			}
			return "", errors.New("not found")
		},
		Disabled: func(id string) string {
			if id == "off" {
				return "user"
			}
			return ""
		},
		MissingSettings: func(m *Manifest) []string {
			if m.ID == "needs" {
				return []string{"needs.token"}
			}
			return nil
		},
	}
}

func TestEvaluateStates(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	first, second := t.TempDir(), t.TempDir()
	writePlugin(t, first, "base", manifest("base", ``))
	writePlugin(t, first, "java", manifest("java", `, "requires": {"commands": ["tar"], "plugins": {"base": ">=1.0.0"}}`))
	writePlugin(t, first, "broken", `{"id": "broken", "version": "x"}`)
	writePlugin(t, first, "off", manifest("off", ``))
	writePlugin(t, first, "needs", manifest("needs", ``))
	writePlugin(t, first, "newer", manifest("newer", `, "requires": {"devbox": ">=2.0.0"}`))
	writePlugin(t, first, "armonly", manifest("armonly", `, "requires": {"arch": ["arm64"], "os": ["linux"]}`))
	writePlugin(t, first, "fedora", manifest("fedora", `, "requires": {"distro": ["fedora"]}`))
	writePlugin(t, first, "nocmd", manifest("nocmd", `, "requires": {"commands": ["tar", "zstd"]}`))
	writePlugin(t, first, "orphan", manifest("orphan", `, "requires": {"plugins": {"missing": ">=1.0.0"}}`))
	writePlugin(t, first, "oldbase", manifest("oldbase", `, "requires": {"plugins": {"base": "^2.0.0"}}`))
	writePlugin(t, first, "onbroken", manifest("onbroken", `, "requires": {"plugins": {"broken": ">=0.0.0"}}`))
	writePlugin(t, first, "cyc-a", manifest("cyc-a", `, "requires": {"plugins": {"cyc-b": ">=1.0.0"}}`))
	writePlugin(t, first, "cyc-b", manifest("cyc-b", `, "requires": {"plugins": {"cyc-a": ">=1.0.0"}}`))
	writePlugin(t, second, "java", manifest("java", ``))

	cands, err := Search(v, []string{first, second, filepath.Join(first, "missing-path")})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Plugin{}
	for _, p := range Evaluate(cands, testEnv()) {
		got[p.ID] = p
	}
	tests := []struct {
		id     string
		state  State
		reason []Reason
	}{
		{"base", StateReady, nil},
		{"java", StateReady, []Reason{{ReasonShadowsDuplicate, filepath.Join(second, "java")}}},
		{"off", StateDisabled, []Reason{{ReasonDisabledBy, "user"}}},
		{"needs", StateIncomplete, []Reason{{ReasonSettingMissing, "needs.token"}}},
		{"newer", StateBlocked, []Reason{{ReasonRequiresDevbox, ">=2.0.0"}}},
		{"armonly", StateBlocked, []Reason{{ReasonRequiresArch, "arm64"}}},
		{"fedora", StateBlocked, []Reason{{ReasonRequiresDistro, "fedora"}}},
		{"nocmd", StateBlocked, []Reason{{ReasonRequiresCommand, "zstd"}}},
		{"orphan", StateBlocked, []Reason{{ReasonRequiresPlugin, "missing >=1.0.0"}}},
		{"oldbase", StateBlocked, []Reason{{ReasonRequiresPlugin, "base ^2.0.0"}}},
		{"onbroken", StateBlocked, []Reason{{ReasonRequiresPlugin, "broken (invalid)"}}},
	}
	for _, tt := range tests {
		p, ok := got[tt.id]
		if !ok {
			t.Errorf("%s: not found", tt.id)
			continue
		}
		if p.State != tt.state || !reflect.DeepEqual(p.Reasons, tt.reason) {
			t.Errorf("%s: state %s %v, want %s %v", tt.id, p.State, p.Reasons, tt.state, tt.reason)
		}
	}
	if b := got["broken"]; b.State != StateInvalid || len(b.Reasons) != 1 || b.Reasons[0].Code != ReasonManifestInvalid {
		t.Errorf("broken: %s %v", b.State, b.Reasons)
	}
	for _, id := range []string{"cyc-a", "cyc-b"} {
		if p := got[id]; p.State != StateBlocked {
			t.Errorf("%s: state %s, want blocked by the cycle", id, p.State)
		}
	}
	if w := Winners(cands); len(w) != len(got)-1 { // broken has no manifest
		t.Errorf("winners = %d, want %d", len(w), len(got)-1)
	}
}

func TestHandlers(t *testing.T) {
	ready := &Plugin{ID: "git", State: StateReady, Manifest: &Manifest{ID: "git", Doc: map[string]any{"on": map[string]any{
		"credential.changed":      []any{"events/apply.sh"},
		"session.finished:java.*": []any{"events/refresh.sh", "--quick"},
	}}}}
	off := &Plugin{ID: "npm", State: StateDisabled, Manifest: &Manifest{ID: "npm", Doc: map[string]any{"on": map[string]any{"credential.changed": []any{"x"}}}}}
	plugins := []*Plugin{ready, off}

	if h := Handlers(plugins, "credential.changed", "company-registry"); len(h) != 1 || h[0].Command[0] != "events/apply.sh" {
		t.Errorf("credential.changed: %v", h)
	}
	if h := Handlers(plugins, "session.finished", "java.install"); len(h) != 1 || !reflect.DeepEqual(h[0].Command, []string{"events/refresh.sh", "--quick"}) {
		t.Errorf("session.finished java: %v", h)
	}
	if h := Handlers(plugins, "session.finished", "node.install"); len(h) != 0 {
		t.Errorf("session.finished node: %v", h)
	}
}
