package sdk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestJSONRemoveRestoresTheFile: a value set in a file with secrets (mode 0600) is
// removed again without a trace; a dry run and a missing value change nothing.
func TestJSONRemoveRestoresTheFile(t *testing.T) {
	e := newEnv(t, false)
	path := filepath.Join(e.vars["HOME"], ".docker", "config.json")
	if err := e.JSONSetMode(path, "/auths/registry.example.com/auth", "eA==", 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new file: mode %v, err %v, want 0600", info.Mode().Perm(), err)
	}
	before, _ := os.ReadFile(path)
	if err := e.JSONSetMode(path, "/auths/other.example.com/auth", "eQ==", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.JSONRemove(path, "/auths/missing.example.com"); err != nil {
		t.Fatal(err)
	}
	if e.changes() != 2 {
		t.Errorf("%d change messages, want 2 (a missing value changes nothing):\n%s", e.changes(), e.msgs)
	}
	if err := e.JSONRemove(path, "/auths/other.example.com"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("after set and remove:\n%s\nwant\n%s", after, before)
	}
	if err := e.JSONRemove(filepath.Join(e.vars["HOME"], "none.json"), "/a"); err != nil {
		t.Errorf("missing file: %v", err)
	}

	dry := newEnv(t, true)
	const doc = "{\n  \"a\": 1,\n  \"b\": 2\n}\n"
	dryPath := filepath.Join(dry.vars["HOME"], "config.json")
	if err := os.WriteFile(dryPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := dry.JSONRemove(dryPath, "/b"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(dryPath); string(after) != doc || dry.changes() != 1 {
		t.Errorf("dry run: file changed or %d change messages, want 1", dry.changes())
	}
}
