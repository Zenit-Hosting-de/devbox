package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func TestNewest(t *testing.T) {
	e := Entry{Versions: []Release{
		{Version: "1.2.0"}, {Version: "2.0.0", Yanked: true}, {Version: "1.10.0"}, {Version: "1.11.0-rc.1"}, {Version: "0.9.0"},
	}}
	tests := []struct{ rng, want string }{
		{"", "1.10.0"},
		{"^1.2.0", "1.10.0"},
		{"<1.10.0", "1.2.0"},
		{">=1.11.0-rc.0", "1.11.0-rc.1"},
		{"^3.0.0", ""},
		{"not a range", ""},
	}
	for _, tt := range tests {
		got, ok := e.Newest(tt.rng)
		if ok != (tt.want != "") || got.Version != tt.want {
			t.Errorf("Newest(%q) = %q, %v; want %q", tt.rng, got.Version, ok, tt.want)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct{ base, ref, want, err string }{
		{"https://example.com/cat/index.json", "pkg/a.tar.gz", "https://example.com/cat/pkg/a.tar.gz", ""},
		{"https://example.com/cat/index.json", "https://cdn.example.com/a.tar.gz", "https://cdn.example.com/a.tar.gz", ""},
		{"https://example.com/cat/index.json", "file:///etc/passwd", "", "only https"},
		{"https://example.com/cat/index.json", "http://example.com/a.tar.gz", "", "only https"},
		{"file:///srv/cat/index.json", "a.tar.gz", "file:///srv/cat/a.tar.gz", ""},
		{"file:///srv/cat/index.json", "https://example.com/a.tar.gz", "https://example.com/a.tar.gz", ""},
	}
	for _, tt := range tests {
		got, err := resolve(tt.base, tt.ref)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("resolve(%q, %q) err = %v, want %q", tt.base, tt.ref, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("resolve(%q, %q) = %q, %v; want %q", tt.base, tt.ref, got, err, tt.want)
		}
	}
}

func writeIndex(t *testing.T, path string, ids ...string) {
	t.Helper()
	plugins := map[string]any{}
	for _, id := range ids {
		plugins[id] = map[string]any{"name": id, "versions": []any{map[string]any{
			"version": "1.0.0", "url": id + ".tar.gz", "digest": "sha256:" + strings.Repeat("a", 64),
			"bundle": id + ".sigstore.json", "publishedAt": "2026-11-09T09:00:00Z",
		}}}
	}
	data, err := jsonfile.Marshal(map[string]any{"generatedAt": "2026-11-09T09:00:00Z", "plugins": plugins})
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonfile.WriteAtomic(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindAndSearch(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	m := &Manager{CacheDir: filepath.Join(dir, "cache"), PluginsDir: filepath.Join(dir, "plugins"), LockPath: filepath.Join(dir, "lock.json"), Validator: v}
	writeIndex(t, filepath.Join(dir, "a.json"), "kubeish", "helmish")
	writeIndex(t, filepath.Join(dir, "b.json"), "kubeish")
	cats := []Catalog{{Name: "a", Source: "file://" + filepath.Join(dir, "a.json")}, {Name: "b", Source: "file://" + filepath.Join(dir, "b.json")}}
	ctx := context.Background()

	if _, _, err := m.Find(ctx, cats, "kubeish", ""); !config.IsCode(err, config.CodeConflict) {
		t.Errorf("two catalogs: %v", err)
	}
	if c, _, err := m.Find(ctx, cats, "kubeish", "b"); err != nil || c.Name != "b" {
		t.Errorf("named: %v %v", c, err)
	}
	if c, _, err := m.Find(ctx, cats, "helmish", ""); err != nil || c.Name != "a" {
		t.Errorf("one catalog: %v %v", c, err)
	}
	if _, _, err := m.Find(ctx, cats, "nope", ""); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("unknown: %v", err)
	}
	hits, errs, err := m.Search(ctx, append(cats, Catalog{Name: "c", Source: "file://" + filepath.Join(dir, "missing.json")}), "")
	if err != nil || len(hits) != 3 || len(errs) != 1 || errs["c"] == nil {
		t.Fatalf("search: %v %v %v", hits, errs, err)
	}
	if hits[0].ID != "helmish" || hits[1].Catalog != "a" || hits[2].Catalog != "b" {
		t.Errorf("order: %v", hits)
	}
	if _, err := m.Prepare(ctx, cats[0], "helmish", Entry{}, ""); err == nil || !strings.Contains(err.Error(), "no trust identity") {
		t.Errorf("unsigned catalog: %v", err)
	}
}
