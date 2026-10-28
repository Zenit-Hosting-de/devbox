package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// ManifestFile is the name of the manifest in every plugin directory.
const ManifestFile = "plugin.json"

// Manifest is a parsed plugin manifest.
type Manifest struct {
	ID  string
	Dir string
	// Doc is the manifest as plain JSON values (numbers as json.Number).
	Doc map[string]any
	// Data is the raw file, for error locations.
	Data []byte
}

// Candidate is a plugin directory found while searching, valid or not.
type Candidate struct {
	Dir      string
	Manifest *Manifest // nil when the manifest is invalid
	Err      error
}

// Search lists the plugin directories below each search path, in path order, and
// parses their manifests. Missing search paths are skipped.
func Search(v *schema.Validator, paths []string) ([]Candidate, error) {
	var out []Candidate
	for _, root := range paths {
		entries, err := os.ReadDir(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read plugin path %s: %w", root, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			dir := filepath.Join(root, name)
			if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
				continue
			}
			m, err := Load(v, dir)
			out = append(out, Candidate{Dir: dir, Manifest: m, Err: err})
		}
	}
	return out, nil
}

// Load reads and validates the manifest of one plugin directory.
func Load(v *schema.Validator, dir string) (*Manifest, error) {
	path := filepath.Join(dir, ManifestFile)
	data, err := os.ReadFile(path) //nolint:gosec // plugin directories come from the configured search paths
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	doc, err := jsonfile.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	plain, ok := jsonfile.Plain(doc).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: the manifest is not a JSON object", path)
	}
	if err := v.Validate("plugin", plain); err != nil {
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			for _, vi := range ve.Violations {
				if vi.Leaf {
					return nil, fmt.Errorf("%s: %s: %s", schema.Location(path, data, vi.Pointer), vi.Keyword, vi.Message)
				}
			}
		}
		return nil, err
	}
	id, _ := plain["id"].(string)
	return &Manifest{ID: id, Dir: dir, Doc: plain, Data: data}, nil
}

// Settings returns the setting fields of the manifest, keyed by the fully qualified key.
func (m *Manifest) Settings() map[string]map[string]any {
	out := map[string]map[string]any{}
	settings, _ := m.Doc["settings"].(map[string]any)
	for name, raw := range settings {
		if field, ok := raw.(map[string]any); ok {
			out[m.ID+"."+name] = field
		}
	}
	return out
}

// Winners returns the valid manifests that win their ID: the first candidate of an
// ID wins even when it is invalid, as in Evaluate.
func Winners(cands []Candidate) []*Manifest {
	seen := map[string]bool{}
	var out []*Manifest
	for _, c := range cands {
		id := filepath.Base(c.Dir)
		if c.Manifest != nil {
			id = c.Manifest.ID
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if c.Manifest != nil {
			out = append(out, c.Manifest)
		}
	}
	return out
}
