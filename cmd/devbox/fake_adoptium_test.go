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
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeAdoptium serves the part of the Adoptium API v3 that the java plugin uses:
// /v3/assets/latest/{feature}/hotspot and the JDK archives it links to.
type fakeAdoptium struct {
	mu       sync.Mutex
	releases map[string]string // feature -> "major.minor.security"
	srv      *httptest.Server
}

func jdkArchive(version string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	root := "jdk-" + version + "/"
	files := map[string]string{
		root + "bin/java": "#!/bin/sh\necho \"openjdk " + version + "\"\n",
		root + "release":  "JAVA_VERSION=\"" + version + "\"\n",
	}
	_ = tw.WriteHeader(&tar.Header{Name: root, Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: root + "bin/", Typeflag: tar.TypeDir, Mode: 0o755})
	for _, name := range []string{root + "bin/java", root + "release"} {
		mode := int64(0o644)
		if strings.HasSuffix(name, "/java") {
			mode = 0o755
		}
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(files[name]))})
		_, _ = tw.Write([]byte(files[name]))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func (f *fakeAdoptium) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 5 && parts[0] == "v3" && parts[1] == "assets" && parts[2] == "latest" && parts[4] == "hotspot":
		f.mu.Lock()
		version, ok := f.releases[parts[3]]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		var major, minor, security int
		_, _ = fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &security)
		sum := sha256.Sum256(jdkArchive(version))
		body := []map[string]any{{
			"binary": map[string]any{"package": map[string]any{
				"link":     f.srv.URL + "/files/jdk-" + version + ".tar.gz",
				"checksum": hex.EncodeToString(sum[:]),
				"name":     "jdk-" + version + ".tar.gz",
			}},
			"release_name": "jdk-" + version,
			"version":      map[string]any{"major": major, "minor": minor, "security": security},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	case len(parts) == 2 && parts[0] == "files" && strings.HasSuffix(parts[1], ".tar.gz"):
		version := strings.TrimSuffix(strings.TrimPrefix(parts[1], "jdk-"), ".tar.gz")
		data := jdkArchive(version)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

var (
	adoptiumMu      sync.Mutex
	adoptiumServers = map[string]*fakeAdoptium{}
)

// cmdAdoptium is the testscript command
//
//	adoptium start                 start a server, set $ADOPTIUM to its /v3 URL
//	adoptium release FEATURE VER   serve VER as the newest build of FEATURE
func cmdAdoptium(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: adoptium start | adoptium release FEATURE VERSION")
	}
	key := ts.Getenv("WORK")
	adoptiumMu.Lock()
	defer adoptiumMu.Unlock()
	switch args[0] {
	case "start":
		f := &fakeAdoptium{releases: map[string]string{}}
		f.srv = httptest.NewServer(f)
		adoptiumServers[key] = f
		ts.Defer(f.srv.Close)
		ts.Setenv("ADOPTIUM", f.srv.URL+"/v3")
	case "release":
		f, ok := adoptiumServers[key]
		if !ok || len(args) != 3 {
			ts.Fatalf("adoptium release: start the server first and give FEATURE VERSION")
		}
		f.mu.Lock()
		f.releases[args[1]] = args[2]
		f.mu.Unlock()
	default:
		ts.Fatalf("adoptium: unknown command %q", args[0])
	}
}
