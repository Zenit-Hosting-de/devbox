package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/rogpeppe/go-internal/testscript"
)

// cmdCatalog is the testscript command
//
//	catalog build SRC OUT IDENTITY   pack every SRC/<id>/ (one folder per plugin and
//	                                 version: SRC/<id>/<version>/plugin.json) into
//	                                 OUT/<id>-<version>.tar.gz with a fake Sigstore
//	                                 bundle naming IDENTITY, and write OUT/index.json
//	catalog sign FILE IDENTITY       write FILE.sigstore.json naming IDENTITY
//
// The fake cosign of the scripts accepts a bundle whose identity matches.
func cmdCatalog(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: catalog build SRC OUT IDENTITY | catalog sign FILE IDENTITY")
	}
	switch args[0] {
	case "build":
		if len(args) != 4 {
			ts.Fatalf("usage: catalog build SRC OUT IDENTITY")
		}
		buildCatalog(ts, ts.MkAbs(args[1]), ts.MkAbs(args[2]), args[3])
	case "sign":
		if len(args) != 3 {
			ts.Fatalf("usage: catalog sign FILE IDENTITY")
		}
		writeBundle(ts, ts.MkAbs(args[1]), args[2])
	default:
		ts.Fatalf("catalog: unknown subcommand %s", args[0])
	}
}

func writeBundle(ts *testscript.TestScript, file, identity string) {
	data, _ := json.Marshal(map[string]string{"identity": identity, "issuer": "https://issuer.example.com"})
	ts.Check(os.WriteFile(file+".sigstore.json", append(data, '\n'), 0o600))
}

func buildCatalog(ts *testscript.TestScript, src, out, identity string) {
	ts.Check(os.MkdirAll(out, 0o750))
	plugins := map[string]any{}
	ids, err := os.ReadDir(src)
	ts.Check(err)
	for _, idEntry := range ids {
		id := idEntry.Name()
		versions, err := os.ReadDir(filepath.Join(src, id))
		ts.Check(err)
		var names []string
		for _, v := range versions {
			names = append(names, v.Name())
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		var releases []any
		for _, version := range names {
			name := id + "-" + version + ".tar.gz"
			data := tarGz(ts, filepath.Join(src, id, version), id)
			ts.Check(os.WriteFile(filepath.Join(out, name), data, 0o600))
			writeBundle(ts, filepath.Join(out, name), identity)
			sum := sha256.Sum256(data)
			releases = append(releases, map[string]any{
				"version": version, "url": name, "digest": "sha256:" + hex.EncodeToString(sum[:]),
				"bundle": name + ".sigstore.json", "publishedAt": "2026-11-09T09:00:00Z",
			})
		}
		plugins[id] = map[string]any{"name": "Plugin " + id, "description": "The " + id + " plugin", "category": "tool", "versions": releases}
	}
	index, err := json.MarshalIndent(map[string]any{"name": "Test catalog", "generatedAt": "2026-11-09T09:00:00Z", "plugins": plugins}, "", "  ")
	ts.Check(err)
	ts.Check(os.WriteFile(filepath.Join(out, "index.json"), append(index, '\n'), 0o600))
}

// tarGz packs dir below the top-level folder id, with fixed times so the digest is stable.
func tarGz(ts *testscript.TestScript, dir, id string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	ts.Check(filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		data, err := os.ReadFile(path) //nolint:gosec // files of the test
		if err != nil {
			return err
		}
		info, _ := d.Info()
		hdr := &tar.Header{Name: id + "/" + filepath.ToSlash(rel), Mode: int64(info.Mode().Perm()), Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	}))
	ts.Check(tw.Close())
	ts.Check(gz.Close())
	return buf.Bytes()
}
