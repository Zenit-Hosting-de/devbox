// Package repofiles lists the files of the repository for the lint tools: tracked and
// untracked files that are not ignored by .gitignore.
package repofiles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// List returns repository-relative, slash-separated paths matching one of the
// extensions (e.g. ".json"), skipping every path below one of the excluded prefixes.
func List(ctx context.Context, exts []string, exclude []string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var files []string
	seen := map[string]bool{}
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" || seen[f] || !hasExt(f, exts) || excluded(f, exclude) {
			continue
		}
		// Tracked files deleted in the working tree are still listed by --cached.
		if _, err := os.Stat(f); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		seen[f] = true
		files = append(files, f)
	}
	return files, nil
}

func hasExt(f string, exts []string) bool {
	ext := filepath.Ext(f)
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

func excluded(f string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(f, p) {
			return true
		}
	}
	return false
}
