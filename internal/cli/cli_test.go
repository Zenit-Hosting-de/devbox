package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// testIO runs commands with HOME in a temporary directory and no system file.
func testIO(t *testing.T, home string) (IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	env := map[string]string{
		"HOME":          home,
		"PATH":          "",
		SystemConfigEnv: filepath.Join(home, "no-system-config.json"),
	}
	var stdout, stderr bytes.Buffer
	return IO{
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  &stderr,
		Getenv:  func(k string) string { return env[k] },
		Environ: func() []string { return nil },
	}, &stdout, &stderr
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestJSONOutputMatchesAPI checks that --json output of the CLI follows the API
// contract (docs/concept.md, "CLI": one JSON document per the API response schema).
func TestJSONOutputMatchesAPI(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	plugins := filepath.Join(home, ".local", "share", "devbox", "plugins")
	writeFile(t, filepath.Join(plugins, "base", "plugin.json"), `{"id": "base", "version": "1.0.0", "name": "Base", "category": "system", "settings": {"mode": {"type": "string", "default": "auto", "label": "Mode"}}}`)
	writeFile(t, filepath.Join(plugins, "broken", "plugin.json"), `{"id": "broken"}`)
	writeFile(t, filepath.Join(plugins, "tool", "plugin.json"), `{"id": "tool", "version": "1.0.0", "name": "Tool", "requires": {"commands": ["tar"]}}`)

	state := filepath.Join(home, ".local", "state", "devbox")
	store := history.New(state, v)
	code := 0
	if err := store.Append(history.Entry{
		ID: "s_01J0000000000000000000", Action: "base.update", Plugin: "base", Title: "Update", User: "kian", Origin: history.OriginUI,
		StartedAt: "2026-01-02T03:04:05Z", EndedAt: "2026-01-02T03:04:09Z", State: "success", ExitCode: &code, Changes: 2, Summary: "done",
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".local", "share", "devbox", "credentials.json"),
		`{"instances": {"registry-main": {"type": "registry", "origin": "user", "fields": {"url": "https://registry.example.com"}, "consumers": {"npm": {}}}}}`)

	tests := []struct {
		args   []string
		target string
	}{
		{[]string{"status", "--json"}, "openapi.StatusReport"},
		{[]string{"history", "--json"}, "openapi.SessionList"},
		{[]string{"cred", "list", "--json"}, "openapi.CredentialList"},
		{[]string{"plugin", "list", "--json"}, "openapi.PluginList"},
		{[]string{"plugin", "info", "base", "--json"}, "openapi.PluginDetail"},
		{[]string{"plugin", "info", "broken", "--json"}, "openapi.PluginDetail"},
		{[]string{"config", "explain", "base.mode", "--json"}, "openapi.SettingExplain"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			sys, stdout, stderr := testIO(t, home)
			if code := Main(tt.args, Build{Version: "1.0.0"}, sys); code != ExitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if err := v.ValidateJSON(tt.target, stdout.Bytes()); err != nil {
				t.Errorf("%v\n%s", err, stdout)
			}
		})
	}
}
