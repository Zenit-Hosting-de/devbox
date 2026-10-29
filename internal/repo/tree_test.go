package repo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	typeflag byte
	body     string
	mode     int64
	link     string
}

func writeTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if e.typeflag == 0 {
			e.typeflag = tar.TypeReg
		}
		if e.mode == 0 {
			e.mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Linkname: e.link}
		if e.typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractTarGzRejects(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		want    string
	}{
		{"parent directory", []tarEntry{{name: "../evil", body: "x"}}, "leaves the target"},
		{"parent in the middle", []tarEntry{{name: "a/../../evil", body: "x"}}, "leaves the target"},
		{"absolute path", []tarEntry{{name: "/etc/evil", body: "x"}}, "absolute"},
		{"symbolic link", []tarEntry{{name: "a/link", typeflag: tar.TypeSymlink, link: "/etc"}}, "not a regular file"},
		{"hard link", []tarEntry{{name: "a/link", typeflag: tar.TypeLink, link: "a/b"}}, "not a regular file"},
		{"character device", []tarEntry{{name: "a/dev", typeflag: tar.TypeChar}}, "not a regular file"},
		{"block device", []tarEntry{{name: "a/dev", typeflag: tar.TypeBlock}}, "not a regular file"},
		{"fifo", []tarEntry{{name: "a/fifo", typeflag: tar.TypeFifo}}, "not a regular file"},
		{"duplicate file", []tarEntry{{name: "a/f", body: "1"}, {name: "a/f", body: "2"}}, "twice"},
		{"backslash", []tarEntry{{name: `a\b`, body: "x"}}, "invalid path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "out")
			err := ExtractTarGz(writeTarGz(t, tt.entries), dest)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
			if _, statErr := os.Stat(dest); statErr == nil {
				t.Errorf("nothing may be extracted from an invalid archive")
			}
		})
	}
}

func TestExtractTarGzNotGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(path, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExtractTarGz(path, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("want an error")
	}
}

func TestExtractTarGzLayout(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		want    map[string]string // path -> content
		exec    string
	}{
		{
			"strips the common top-level directory",
			[]tarEntry{
				{name: "pkg-1.0/", typeflag: tar.TypeDir},
				{name: "pkg-1.0/plugins/a/plugin.json", body: "{}"},
				{name: "pkg-1.0/plugins/a/run.sh", body: "#!/bin/sh\n", mode: 0o755},
			},
			map[string]string{"plugins/a/plugin.json": "{}", "plugins/a/run.sh": "#!/bin/sh\n"},
			"plugins/a/run.sh",
		},
		{
			"keeps several top-level entries",
			[]tarEntry{{name: "./a/x", body: "1"}, {name: "b/y", body: "2"}},
			map[string]string{"a/x": "1", "b/y": "2"},
			"",
		},
		{
			"keeps a file at the top level",
			[]tarEntry{{name: "top", body: "1"}, {name: "dir/y", body: "2"}},
			map[string]string{"top": "1", "dir/y": "2"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "out")
			if err := ExtractTarGz(writeTarGz(t, tt.entries), dest); err != nil {
				t.Fatal(err)
			}
			for rel, content := range tt.want {
				got, err := os.ReadFile(filepath.Join(dest, rel))
				if err != nil || string(got) != content {
					t.Errorf("%s = %q, %v; want %q", rel, got, err, content)
				}
			}
			if tt.exec != "" {
				info, err := os.Stat(filepath.Join(dest, tt.exec))
				if err != nil || !executable(info.Mode()) {
					t.Errorf("%s must keep the executable bit: %v, %v", tt.exec, info, err)
				}
			}
		})
	}
}

func writeTree(t *testing.T, root string, files map[string]string, execs ...string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		for _, e := range execs {
			if e == rel {
				mode = 0o755
			}
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil { //nolint:gosec // test files
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil { //nolint:gosec // test files, independent of the umask
			t.Fatal(err)
		}
	}
}

func TestTreeDigest(t *testing.T) {
	files := map[string]string{"plugin.json": "{}", "actions/run.sh": "echo", "i18n/de.json": "{}"}
	digest := func(files map[string]string, execs ...string) string {
		root := t.TempDir()
		writeTree(t, root, files, execs...)
		d, err := TreeDigest(root)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	base := digest(files, "actions/run.sh")
	if !strings.HasPrefix(base, "sha256:") || len(base) != len("sha256:")+64 {
		t.Fatalf("digest = %q", base)
	}
	tests := []struct {
		name  string
		files map[string]string
		execs []string
		same  bool
	}{
		{"same content in another directory", files, []string{"actions/run.sh"}, true},
		{"changed content", map[string]string{"plugin.json": "{ }", "actions/run.sh": "echo", "i18n/de.json": "{}"}, []string{"actions/run.sh"}, false},
		{"executable bit lost", files, nil, false},
		{"renamed file", map[string]string{"plugin.json": "{}", "actions/go.sh": "echo", "i18n/de.json": "{}"}, []string{"actions/go.sh"}, false},
		{"extra file", map[string]string{"plugin.json": "{}", "actions/run.sh": "echo", "i18n/de.json": "{}", "x": ""}, []string{"actions/run.sh"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := digest(tt.files, tt.execs...); (got == base) != tt.same {
				t.Errorf("digest equal = %v, want %v", got == base, tt.same)
			}
		})
	}
	// group and other bits do not matter
	root := t.TempDir()
	writeTree(t, root, files, "actions/run.sh")
	if err := os.Chmod(filepath.Join(root, "actions", "run.sh"), 0o700); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}
	if got, _ := TreeDigest(root); got != base {
		t.Error("the digest depends on group and other permission bits")
	}
}

func TestTreeRejectsLinks(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"a/plugin.json": "{}"})
	if err := os.Symlink("/etc/hostname", filepath.Join(src, "a", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := TreeDigest(src); err == nil || !strings.Contains(err.Error(), "a/link") {
		t.Errorf("TreeDigest: %v", err)
	}
	if _, err := CopyTree(src, filepath.Join(t.TempDir(), "out")); err == nil || !strings.Contains(err.Error(), "a/link") {
		t.Errorf("CopyTree: %v", err)
	}
}

func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"a/plugin.json": "{}", "a/run.sh": "x", ".git/config": "secret"}, "a/run.sh")
	dest := filepath.Join(t.TempDir(), "out")
	digest, err := CopyTree(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		t.Error(".git was copied")
	}
	info, err := os.Stat(filepath.Join(dest, "a", "run.sh"))
	if err != nil || !executable(info.Mode()) {
		t.Errorf("run.sh: %v, %v", info, err)
	}
	if again, _ := TreeDigest(dest); again != digest {
		t.Error("the digest of the copy differs from the returned one")
	}
	if _, err := CopyTree(filepath.Join(src, "missing"), filepath.Join(t.TempDir(), "o")); err == nil {
		t.Error("a missing source directory must be an error")
	}
}
