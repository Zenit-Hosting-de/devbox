package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// executableDirs hold scripts that the runner starts; files there get mode 0755.
var executableDirs = []string{"checks", "actions", "consume", "events", "bin"}

// fileMode returns the mode for an extracted file: scripts are executable.
func fileMode(name string) fs.FileMode {
	parts := strings.Split(name, "/")
	if len(parts) > 2 {
		for _, d := range executableDirs {
			if parts[1] == d {
				return 0o755
			}
		}
	}
	switch path.Ext(name) {
	case ".sh", ".bash":
		return 0o755
	}
	return 0o644
}

// ContentHash returns a short hash of every file in fsys, so a new build with changed
// plugins extracts into a new directory.
func ContentHash(fsys fs.FS) (string, error) {
	h := sha256.New()
	var names []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	for _, n := range names {
		data, err := fs.ReadFile(fsys, n)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", n, len(data))
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// ExtractBuiltin unpacks the plugin directories of fsys (one per top-level directory)
// into root/<version>-<hash>, once, and removes older extractions. It returns the
// directory to add to the plugin search paths.
func ExtractBuiltin(fsys fs.FS, root, version string) (string, error) {
	hash, err := ContentHash(fsys)
	if err != nil {
		return "", err
	}
	name := strings.ReplaceAll(version, "/", "_") + "-" + hash
	dest := filepath.Join(root, name)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".extract-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return err
		}
		target := filepath.Join(tmp, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !strings.Contains(p, "/") {
			return nil // files next to the plugin directories (README, embed.go)
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, fileMode(p)) //nolint:gosec // scripts must be executable
	})
	if err != nil {
		return "", fmt.Errorf("extract built-in plugins: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			return dest, nil // extracted concurrently by another process
		}
		return "", err
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && e.Name() != name && !strings.HasPrefix(e.Name(), ".") {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
	return dest, nil
}
