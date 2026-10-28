package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// DEVBOX_TEST_VERSION lets a flow run the test binary as a released version (the build
// of the tests is a development build). The plugin runner passes a fixed environment to
// plugins, so the variable never reaches them.
func init() {
	if v := os.Getenv("DEVBOX_TEST_VERSION"); v != "" {
		version = v
	}
}

// fakeReleases serves the part of the GitHub releases API that the system plugin uses:
// the release list and the assets it links to (archives and checksums.txt).
type fakeReleases struct {
	mu       sync.Mutex
	releases []*fakeRelease // in the order of publishing; the list shows the newest first
	srv      *httptest.Server
}

type fakeRelease struct {
	version    string
	prerelease bool
	// component tags the release like release-please with a component: devbox-vX.Y.Z.
	component bool
	draft     bool
	noAssets  bool
	corrupt   bool // checksums.txt lists a wrong checksum
}

var fakeArches = []string{"amd64", "arm64"}

func fakeArchiveName(version, arch string) string {
	return "devbox_" + version + "_linux_" + arch + ".tar.gz"
}

// fakeBinary is the "devbox" of a release: a script that prints its version.
func fakeBinary(version string) []byte {
	return []byte("#!/bin/sh\necho \"devbox " + version + "\"\n")
}

// fakeTarball packs the binary like GoReleaser: the file devbox at the top level.
func fakeTarball(version string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	bin := fakeBinary(version)
	_ = tw.WriteHeader(&tar.Header{Name: "LICENSE", Typeflag: tar.TypeReg, Mode: 0o644, Size: 8})
	_, _ = tw.Write([]byte("license\n"))
	_ = tw.WriteHeader(&tar.Header{Name: "devbox", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(bin))})
	_, _ = tw.Write(bin)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func (f *fakeReleases) find(version string) *fakeRelease {
	for _, r := range f.releases {
		if r.version == version {
			return r
		}
	}
	return nil
}

func (f *fakeReleases) checksums(r *fakeRelease) string {
	var b strings.Builder
	for _, arch := range fakeArches {
		sum := sha256.Sum256(fakeTarball(r.version))
		s := hex.EncodeToString(sum[:])
		if r.corrupt {
			s = strings.Repeat("0", len(s))
		}
		fmt.Fprintf(&b, "%s  %s\n", s, fakeArchiveName(r.version, arch))
	}
	return b.String()
}

func (f *fakeReleases) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/repos/example/devbox/releases":
		list := []map[string]any{}
		for i := len(f.releases) - 1; i >= 0; i-- {
			rel := f.releases[i]
			assets := []map[string]any{}
			if !rel.noAssets {
				names := []string{"checksums.txt"}
				for _, arch := range fakeArches {
					names = append(names, fakeArchiveName(rel.version, arch))
				}
				for _, n := range names {
					assets = append(assets, map[string]any{
						"name":                 n,
						"browser_download_url": f.srv.URL + "/download/v" + rel.version + "/" + n,
					})
				}
			}
			list = append(list, map[string]any{
				"tag_name":   rel.tag(),
				"prerelease": rel.prerelease,
				"draft":      rel.draft,
				"assets":     assets,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	case len(parts) == 3 && parts[0] == "download":
		rel := f.find(strings.TrimPrefix(parts[1], "v"))
		if rel == nil || rel.noAssets {
			http.NotFound(w, r)
			return
		}
		if parts[2] == "checksums.txt" {
			_, _ = w.Write([]byte(f.checksums(rel)))
			return
		}
		for _, arch := range fakeArches {
			if parts[2] == fakeArchiveName(rel.version, arch) {
				data := fakeTarball(rel.version)
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				_, _ = w.Write(data)
				return
			}
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

var (
	releasesMu      sync.Mutex
	releasesServers = map[string]*fakeReleases{}
)

// cmdReleases is the testscript command
//
//	releases start                          start a server, set $RELEASES to its release list
//	releases add VERSION [FLAG…]            publish a release; FLAG is prerelease, draft, noassets, corrupt or component
//	releases corrupt VERSION                make checksums.txt of a published release wrong
func cmdReleases(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: releases start | releases add VERSION [prerelease|draft|noassets|corrupt|component]… | releases corrupt VERSION")
	}
	key := ts.Getenv("WORK")
	releasesMu.Lock()
	defer releasesMu.Unlock()
	if args[0] == "start" {
		f := &fakeReleases{}
		f.srv = httptest.NewServer(f)
		releasesServers[key] = f
		ts.Defer(f.srv.Close)
		ts.Setenv("RELEASES", f.srv.URL+"/repos/example/devbox/releases")
		return
	}
	f, ok := releasesServers[key]
	if !ok {
		ts.Fatalf("releases %s: start the server first", args[0])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case args[0] == "add" && len(args) >= 2:
		rel := &fakeRelease{version: args[1]}
		for _, flag := range args[2:] {
			switch flag {
			case "prerelease":
				rel.prerelease = true
			case "component":
				rel.component = true
			case "draft":
				rel.draft = true
			case "noassets":
				rel.noAssets = true
			case "corrupt":
				rel.corrupt = true
			default:
				ts.Fatalf("releases add: unknown flag %q", flag)
			}
		}
		f.releases = append(f.releases, rel)
	case args[0] == "corrupt" && len(args) == 2:
		rel := f.find(args[1])
		if rel == nil {
			ts.Fatalf("releases corrupt: no release %s", args[1])
		}
		rel.corrupt = true
	default:
		ts.Fatalf("releases: unknown command %q", strings.Join(args, " "))
	}
}

// tag is the tag name of the release: vX.Y.Z, or devbox-vX.Y.Z for component tags.
func (r *fakeRelease) tag() string {
	if r.component {
		return "devbox-v" + r.version
	}
	return "v" + r.version
}
