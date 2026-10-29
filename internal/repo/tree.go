package repo

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
)

// Limits for what a repository may contain.
const (
	// MaxDownloadBytes is the largest tarball that is downloaded (100 MiB).
	MaxDownloadBytes = 100 << 20
	// MaxTreeBytes is the largest sum of file sizes that is copied or extracted, which
	// also bounds archives that expand a lot.
	MaxTreeBytes = 1 << 30
)

// File and directory modes of a snapshot: the owner only; scripts keep the
// executable bit.
const (
	modeDir  fs.FileMode = 0o750
	modeFile fs.FileMode = 0o600
	modeExec fs.FileMode = 0o700
)

func invalidf(format string, a ...any) error {
	return &config.Error{Code: config.CodeValidation, Detail: fmt.Sprintf(format, a...)}
}

func executable(mode fs.FileMode) bool { return mode.Perm()&0o100 != 0 }

// TreeDigest returns the SHA-256 digest of a directory tree as "sha256:<hex>": over
// the files in the order of their relative slash paths, each as path, executable bit
// (755 or 644), size and content. Empty directories do not count and nothing but the
// executable bit of the mode does, so the digest is the same on every machine. Links
// and special files are an error.
func TreeDigest(root string) (string, error) {
	files, err := listTree(root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		mode := "644"
		if executable(f.mode) {
			mode = "755"
		}
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00", f.rel, mode, f.size)
		in, err := os.Open(f.abs) //nolint:gosec // below the snapshot directory
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, in)
		_ = in.Close()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", f.rel, err)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

type treeFile struct {
	rel, abs string
	mode     fs.FileMode
	size     int64
}

// listTree lists the regular files below root, sorted by relative path. A symbolic
// link or special file is an error.
func listTree(root string) ([]treeFile, error) {
	var files []treeFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return invalidf("%s is a link or a special file; only regular files and directories are allowed", filepath.ToSlash(rel))
		}
		files = append(files, treeFile{rel: filepath.ToSlash(rel), abs: p, mode: info.Mode(), size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, nil
}

// CopyTree copies the directory src to dest (which must not exist). Only regular
// files and directories are copied; a symbolic link or special file is an error. A
// ".git" directory is skipped. It returns the SHA-256 digest of the copy, which is
// the revision of a local directory.
func CopyTree(src, dest string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", invalidf("source directory %s: %v", src, err)
	}
	if !info.IsDir() {
		return "", invalidf("%s is not a directory", src)
	}
	var total int64
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			if d.Name() == ".git" && rel != "." {
				return fs.SkipDir
			}
			return os.MkdirAll(target, modeDir)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return invalidf("%s is a link or a special file; only regular files and directories are allowed", filepath.ToSlash(rel))
		}
		if total += fi.Size(); total > MaxTreeBytes {
			return invalidf("the directory is larger than %d MiB", MaxTreeBytes>>20)
		}
		return copyFile(p, target, fi.Mode())
	})
	if err != nil {
		return "", err
	}
	return TreeDigest(dest)
}

func copyFile(src, dest string, mode fs.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // the source directory the user configured
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return writeFile(dest, in, mode, -1)
}

// writeFile creates dest exclusively with the snapshot mode and fills it from r;
// limit < 0 means no limit on the size.
func writeFile(dest string, r io.Reader, mode fs.FileMode, limit int64) error {
	perm := modeFile
	if executable(mode) {
		perm = modeExec
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // inside the snapshot directory
	if err != nil {
		return err
	}
	if limit >= 0 {
		r = io.LimitReader(r, limit)
	}
	_, err = io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// ExtractTarGz unpacks a gzip-compressed tar archive into dest. Only regular files
// and directories are extracted, with the executable bit kept. An absolute path, a
// ".." element, a link (symbolic or hard), a device, a FIFO or a duplicate entry makes
// the whole archive invalid. When all entries lie below one top-level directory (as in
// tarballs from GitHub), that directory is stripped.
func ExtractTarGz(archive, dest string) error {
	prefix, err := scanArchive(archive)
	if err != nil {
		return err
	}
	f, err := os.Open(archive) //nolint:gosec // the file DevBox just downloaded
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return invalidf("the download is not a gzip-compressed tar archive: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(dest, modeDir); err != nil {
		return err
	}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return invalidf("the archive is damaged: %v", err)
		}
		name, skip := entryName(hdr, prefix)
		if skip {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, modeDir); err != nil {
				return err
			}
			continue
		}
		if total += hdr.Size; total > MaxTreeBytes {
			return invalidf("the archive expands to more than %d MiB", MaxTreeBytes>>20)
		}
		if err := os.MkdirAll(filepath.Dir(target), modeDir); err != nil {
			return err
		}
		if err := writeFile(target, tr, fs.FileMode(hdr.Mode), hdr.Size); err != nil { //nolint:gosec // permission bits only
			return fmt.Errorf("extract %s: %w", name, err)
		}
	}
}

// scanArchive checks every entry of the archive and returns the top-level directory
// that is stripped, or "".
func scanArchive(archive string) (string, error) {
	f, err := os.Open(archive) //nolint:gosec // the file DevBox just downloaded
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", invalidf("the download is not a gzip-compressed tar archive: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	top := ""
	single := true
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", invalidf("the archive is damaged: %v", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name, err := cleanEntry(hdr)
		if err != nil {
			return "", err
		}
		if name == "" {
			continue
		}
		if hdr.Typeflag != tar.TypeDir {
			if seen[name] {
				return "", invalidf("the archive contains %s twice", name)
			}
			seen[name] = true
		}
		first, _, nested := strings.Cut(name, "/")
		if !nested && hdr.Typeflag != tar.TypeDir {
			single = false // a file at the top level
			continue
		}
		if top == "" {
			top = first
		} else if top != first {
			single = false
		}
	}
	if !single {
		return "", nil
	}
	return top, nil
}

// cleanEntry validates the type and the path of an archive entry and returns the path
// in slash form without leading "./"; "" for the root directory.
func cleanEntry(hdr *tar.Header) (string, error) {
	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeDir:
	default:
		return "", invalidf("the archive contains %s, which is not a regular file or directory (type %q)", hdr.Name, string(hdr.Typeflag))
	}
	name := hdr.Name
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) {
		return "", invalidf("the archive contains an invalid path %q", name)
	}
	if path.IsAbs(name) {
		return "", invalidf("the archive contains the absolute path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", invalidf("the archive contains the path %q, which leaves the target directory", name)
		}
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

// entryName returns the path of an entry below the target directory, with the
// stripped prefix removed; skip is set for entries that produce nothing.
func entryName(hdr *tar.Header, prefix string) (name string, skip bool) {
	if hdr.Typeflag == tar.TypeXGlobalHeader {
		return "", true
	}
	name, err := cleanEntry(hdr)
	if err != nil || name == "" { // scanArchive has checked every entry
		return "", true
	}
	if prefix != "" {
		if name == prefix {
			return "", true
		}
		name = strings.TrimPrefix(name, prefix+"/")
	}
	return name, false
}
