package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExtractBuiltin(t *testing.T) {
	fsys := fstest.MapFS{
		"embed.go":               {Data: []byte("package plugins")},
		"demo/plugin.json":       {Data: []byte(`{"id":"demo"}`)},
		"demo/checks/state.sh":   {Data: []byte("#!/bin/sh\n")},
		"demo/lib/common.sh":     {Data: []byte("x=1\n")},
		"demo/i18n/de.json":      {Data: []byte("{}")},
		"demo/actions/run.py":    {Data: []byte("print()\n")},
		"other/tests/other.bats": {Data: []byte("@test x {}\n")},
		"other/plugin.json":      {Data: []byte(`{"id":"other"}`)},
	}
	root := t.TempDir()
	dir, err := ExtractBuiltin(fsys, root, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]os.FileMode{
		"demo/plugin.json":     0o644,
		"demo/checks/state.sh": 0o755,
		"demo/lib/common.sh":   0o755,
		"demo/i18n/de.json":    0o644,
		"demo/actions/run.py":  0o755,
	}
	for name, want := range modes {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v", name, info.Mode().Perm(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "embed.go")); !os.IsNotExist(err) {
		t.Error("top-level files must not be extracted")
	}

	// Same content: same directory, no second extraction.
	again, err := ExtractBuiltin(fsys, root, "1.0.0")
	if err != nil || again != dir {
		t.Errorf("again = %s, %v; want %s", again, err, dir)
	}
	// Changed content: new directory, the old one is removed.
	fsys["demo/plugin.json"] = &fstest.MapFile{Data: []byte(`{"id":"demo","x":1}`)}
	changed, err := ExtractBuiltin(fsys, root, "1.0.0")
	if err != nil || changed == dir {
		t.Fatalf("changed = %s, %v", changed, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("the previous extraction was kept")
	}
}
