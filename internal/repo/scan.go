package repo

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// ScanPlugins lists the plugin folders directly below root (each folder with a
// plugin.json), sorted by folder name, with the digest of the folder and the
// permissions and required commands of the manifest. A folder whose manifest is
// invalid is listed with its error. Paths in errors are relative to root.
func ScanPlugins(v *schema.Validator, root string) ([]Plugin, error) {
	cands, err := plugin.Search(v, []string{root})
	if err != nil {
		return nil, err
	}
	out := make([]Plugin, 0, len(cands))
	seen := map[string]string{}
	for _, c := range cands {
		digest, err := TreeDigest(c.Dir)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", filepath.Base(c.Dir), err)
		}
		p := Plugin{ID: filepath.Base(c.Dir), Digest: digest, Permissions: []Permission{}, RequiresCommands: []string{}}
		if c.Manifest == nil {
			p.Error = strings.ReplaceAll(fmt.Sprint(c.Err), root+string(filepath.Separator), "")
		} else {
			fill(&p, c.Manifest)
		}
		if first, dup := seen[p.ID]; dup {
			p.Error = fmt.Sprintf("the ID %s is already used by the folder %s", p.ID, first)
		} else {
			seen[p.ID] = filepath.Base(c.Dir)
		}
		out = append(out, p)
	}
	return out, nil
}

// Limits of the search for folders with plugins in a snapshot without plugins at the
// configured path.
const (
	suggestDepth = 3
	suggestMax   = 10
)

// SuggestPaths looks for folders below snapshot (at most suggestDepth levels deep,
// hidden folders skipped) that hold plugin folders, that is sub-folders with a
// plugin.json. The paths are relative to snapshot, in slash form and sorted; the root
// of the snapshot and the folder skip (the one that was scanned) are left out, and at
// most suggestMax paths are returned.
func SuggestPaths(snapshot, skip string) []string {
	var found []string
	var walk func(rel string, depth int)
	walk = func(rel string, depth int) {
		entries, err := os.ReadDir(subDir(snapshot, rel))
		if err != nil {
			return
		}
		holds := false
		for _, de := range entries {
			if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
				continue
			}
			child := path.Join(rel, de.Name())
			if _, err := os.Stat(filepath.Join(subDir(snapshot, child), plugin.ManifestFile)); err == nil {
				holds = true
			} else if depth < suggestDepth {
				walk(child, depth+1)
			}
		}
		if holds && rel != "" && rel != skip {
			found = append(found, rel)
		}
	}
	walk("", 0)
	sort.Strings(found)
	if len(found) > suggestMax {
		found = found[:suggestMax]
	}
	return found
}

func fill(p *Plugin, m *plugin.Manifest) {
	p.ID = m.ID
	p.Name, _ = m.Doc["name"].(string)
	p.Version, _ = m.Doc["version"].(string)
	p.Description, _ = m.Doc["description"].(string)
	p.Permissions = permissionsOf(m.Doc)
	if req, ok := m.Doc["requires"].(map[string]any); ok {
		p.RequiresCommands = append(p.RequiresCommands, stringList(req["commands"])...)
	}
}

// permissionsOf flattens the "permissions" of a manifest, in the order network,
// files.read, files.write, sudo, credentials.
func permissionsOf(doc map[string]any) []Permission {
	out := []Permission{}
	perms, _ := doc["permissions"].(map[string]any)
	add := func(kind string, values []string) {
		for _, v := range values {
			out = append(out, Permission{Kind: kind, Value: v})
		}
	}
	add(PermNetwork, stringList(perms["network"]))
	files, _ := perms["files"].(map[string]any)
	add(PermFilesRead, stringList(files["read"]))
	add(PermFilesWrite, stringList(files["write"]))
	if sudo, _ := perms["sudo"].(bool); sudo {
		add(PermSudo, []string{"true"})
	}
	add(PermCredentials, stringList(perms["credentials"]))
	return out
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// subDir returns the directory below a snapshot that holds the plugin folders.
func subDir(snapshot, sub string) string {
	if sub == "" {
		return snapshot
	}
	return filepath.Join(snapshot, filepath.FromSlash(sub))
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
