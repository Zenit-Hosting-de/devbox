package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func TestSetPluginDisabled(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "devbox", "config.json")
	// switching on something that is not off must not create the file
	if changed, err := SetPluginDisabled(v, path, "demo", false); err != nil || changed {
		t.Fatalf("changed = %v, %v", changed, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a no-op created the file")
	}
	steps := []struct {
		id       string
		disabled bool
		changed  bool
		want     []string
	}{
		{"demo", true, true, []string{"demo"}},
		{"demo", true, false, []string{"demo"}},
		{"other", true, true, []string{"demo", "other"}},
		{"demo", false, true, []string{"other"}},
		{"other", false, true, nil},
	}
	for _, s := range steps {
		changed, err := SetPluginDisabled(v, path, s.id, s.disabled)
		if err != nil || changed != s.changed {
			t.Fatalf("%v: changed = %v, %v", s, changed, err)
		}
		f, err := ReadFile(v, "config", path, false)
		if err != nil {
			t.Fatal(err)
		}
		plugins, has := f.Plain()["plugins"].(map[string]any)
		var got []string
		if list, ok := plugins["disabled"].([]any); ok {
			for _, id := range list {
				got = append(got, id.(string)) //nolint:errcheck,forcetypeassert // checked by the schema
			}
		}
		if strings.Join(got, ",") != strings.Join(s.want, ",") {
			t.Errorf("%v: disabled = %v", s, got)
		}
		if len(s.want) == 0 && has {
			t.Errorf("%v: an empty plugins object must be removed: %v", s, plugins)
		}
	}
}

func TestSetAndRemoveRepository(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	fields := [][2]any{{"kind", "plugins"}, {"source", "file:///srv/plugins"}, {"trust", "prompt"}}
	if err := SetRepository(v, path, "team", fields); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFile(v, "config", path, false)
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := f.Plain()["repositories"].(map[string]any)
	if entry, _ := repos["team"].(map[string]any); entry["source"] != "file:///srv/plugins" {
		t.Errorf("repositories = %v", repos)
	}
	// an entry that does not follow the schema is refused and nothing is written
	bad := [][2]any{{"kind", "plugins"}, {"source", "ftp://x.example.com"}}
	if err := SetRepository(v, path, "bad-one", bad); err == nil {
		t.Error("a source with an unknown scheme must be refused")
	}
	if err := SetRepository(v, path, "X", fields); err == nil {
		t.Error("an invalid name must be refused")
	}
	if removed, err := RemoveRepository(v, path, "nope"); err != nil || removed {
		t.Errorf("remove unknown: %v, %v", removed, err)
	}
	if removed, err := RemoveRepository(v, path, "team"); err != nil || !removed {
		t.Errorf("remove: %v, %v", removed, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "repositories") {
		t.Errorf("file = %s", data)
	}
}
