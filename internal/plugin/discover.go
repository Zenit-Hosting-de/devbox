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

// Origin kinds of a plugin.
const (
	OriginBuiltin    = "builtin"
	OriginPath       = "path"
	OriginRepository = "repository"
)

// Origin says where a plugin was found (api/openapi.json, PluginOrigin).
type Origin struct {
	Kind string `json:"kind"`
	// Path is the search path of a "path" origin.
	Path string `json:"path,omitempty"`
	// Repository and Revision (short) name the active revision of a "repository" origin.
	Repository string `json:"repository,omitempty"`
	Revision   string `json:"revision,omitempty"`
}

// String is the short text form: builtin, path:<dir> or repository:<name>@<revision>.
func (o Origin) String() string {
	switch o.Kind {
	case OriginPath:
		return OriginPath + ":" + o.Path
	case OriginRepository:
		return OriginRepository + ":" + o.Repository + "@" + o.Revision
	}
	return o.Kind
}

// Root is a directory with plugin directories and where they come from.
type Root struct {
	Dir    string
	Origin Origin
}

// Candidate is a plugin directory found while searching, valid or not.
type Candidate struct {
	Dir      string
	Manifest *Manifest // nil when the manifest is invalid
	Err      error
	Origin   Origin
}

// Search lists the plugin directories below each search path, in path order, and
// parses their manifests. Missing search paths are skipped. All plugins have the
// origin "path".
func Search(v *schema.Validator, paths []string) ([]Candidate, error) {
	roots := make([]Root, 0, len(paths))
	for _, p := range paths {
		roots = append(roots, Root{Dir: p, Origin: Origin{Kind: OriginPath, Path: p}})
	}
	return SearchRoots(v, roots)
}

// SearchRoots is Search with an origin per search path.
func SearchRoots(v *schema.Validator, roots []Root) ([]Candidate, error) {
	var out []Candidate
	for _, r := range roots {
		root := r.Dir
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
			out = append(out, Candidate{Dir: dir, Manifest: m, Err: err, Origin: r.Origin})
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
