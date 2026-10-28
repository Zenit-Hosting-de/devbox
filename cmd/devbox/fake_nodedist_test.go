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
	"sort"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// nodeArchs are the platforms the fake distribution server offers.
var nodeArchs = []string{"linux-arm64", "linux-x64"}

// fakeNodeDist serves the part of nodejs.org/dist that the node plugin uses:
// /dist/index.json, /dist/vX.Y.Z/SHASUMS256.txt and the linux tarballs.
type fakeNodeDist struct {
	mu       sync.Mutex
	releases map[string]string // version without "v" -> LTS codename, "" for none
	corrupt  map[string]bool   // versions whose archives do not match SHASUMS256.txt
	srv      *httptest.Server
}

// fakeNodeScript is a stand-in for bin/node. It prints its version.
func fakeNodeScript(version string) string {
	return "#!/bin/sh\necho \"v" + version + "\"\n"
}

// fakeNpmScript is a stand-in for bin/npm. "npm install -g NAME" creates
// lib/node_modules/NAME next to the bin directory and logs the call.
const fakeNpmScript = `#!/bin/sh
prefix=$(cd "$(dirname "$0")/.." && pwd)
case "$1" in
--version) echo "10.0.0" ;;
install)
	shift
	[ "$1" = "-g" ] || { echo "fake npm: only install -g is supported" >&2; exit 1; }
	shift
	for spec in "$@"; do
		name=${spec#@}
		case "$name" in *@*) name=${name%@*} ;; esac
		case "$spec" in @*) name=@$name ;; esac
		mkdir -p "$prefix/lib/node_modules/$name"
		echo "{\"name\":\"$name\"}" >"$prefix/lib/node_modules/$name/package.json"
		echo "install -g $spec" >>"$prefix/npm.log"
	done
	;;
*) echo "fake npm: unsupported: $*" >&2; exit 1 ;;
esac
`

func nodeArchive(version, platform string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	root := "node-v" + version + "-" + platform + "/"
	for _, d := range []string{"", "bin/", "lib/", "lib/node_modules/", "lib/node_modules/npm/"} {
		_ = tw.WriteHeader(&tar.Header{Name: root + d, Typeflag: tar.TypeDir, Mode: 0o755})
	}
	for _, f := range []struct{ name, body string }{
		{"bin/node", fakeNodeScript(version)},
		{"bin/npm", fakeNpmScript},
		{"lib/node_modules/npm/package.json", "{\"name\":\"npm\"}\n"},
	} {
		mode := int64(0o755)
		if strings.HasSuffix(f.name, ".json") {
			mode = 0o644
		}
		_ = tw.WriteHeader(&tar.Header{Name: root + f.name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(f.body))})
		_, _ = tw.Write([]byte(f.body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func nodeVersionLess(a, b string) bool {
	var a1, a2, a3, b1, b2, b3 int
	_, _ = fmt.Sscanf(a, "%d.%d.%d", &a1, &a2, &a3)
	_, _ = fmt.Sscanf(b, "%d.%d.%d", &b1, &b2, &b3)
	if a1 != b1 {
		return a1 < b1
	}
	if a2 != b2 {
		return a2 < b2
	}
	return a3 < b3
}

// index returns the releases like dist/index.json: newest first, "lts" is the
// codename or false.
func (f *fakeNodeDist) index() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	versions := make([]string, 0, len(f.releases))
	for v := range f.releases {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return nodeVersionLess(versions[j], versions[i]) })
	out := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		var lts any = false
		if name := f.releases[v]; name != "" {
			lts = name
		}
		out = append(out, map[string]any{
			"version": "v" + v, "date": "2024-01-01", "files": nodeArchs, "npm": "10.0.0", "lts": lts,
		})
	}
	return out
}

func (f *fakeNodeDist) has(version string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.releases[version]
	return ok
}

func (f *fakeNodeDist) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "dist" {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "index.json":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.index())
	case len(parts) == 3 && strings.HasPrefix(parts[1], "v") && f.has(strings.TrimPrefix(parts[1], "v")):
		version := strings.TrimPrefix(parts[1], "v")
		prefix := "node-v" + version + "-"
		switch {
		case parts[2] == "SHASUMS256.txt":
			var b strings.Builder
			for _, p := range nodeArchs {
				sum := sha256.Sum256(nodeArchive(version, p))
				fmt.Fprintf(&b, "%s  %s%s.tar.gz\n", hex.EncodeToString(sum[:]), prefix, p)
			}
			_, _ = w.Write([]byte(b.String()))
		case strings.HasPrefix(parts[2], prefix) && strings.HasSuffix(parts[2], ".tar.gz"):
			platform := strings.TrimSuffix(strings.TrimPrefix(parts[2], prefix), ".tar.gz")
			data := nodeArchive(version, platform)
			f.mu.Lock()
			if f.corrupt[version] {
				data = append(data, 0)
			}
			f.mu.Unlock()
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

var (
	nodeDistMu      sync.Mutex
	nodeDistServers = map[string]*fakeNodeDist{}
)

// cmdNodeDist is the testscript command
//
//	nodedist start                  start a server, set $NODEDIST to its /dist URL
//	nodedist release VERSION [LTS]  serve VERSION (e.g. 22.11.0), optionally as an LTS
//	                                release with the given codename
//	nodedist corrupt VERSION        serve archives of VERSION that fail the checksum
func cmdNodeDist(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: nodedist start | release VERSION [LTS] | corrupt VERSION")
	}
	key := ts.Getenv("WORK")
	nodeDistMu.Lock()
	defer nodeDistMu.Unlock()
	switch args[0] {
	case "start":
		f := &fakeNodeDist{releases: map[string]string{}, corrupt: map[string]bool{}}
		f.srv = httptest.NewServer(f)
		nodeDistServers[key] = f
		ts.Defer(f.srv.Close)
		ts.Setenv("NODEDIST", f.srv.URL+"/dist")
	case "release":
		f, ok := nodeDistServers[key]
		if !ok || len(args) < 2 || len(args) > 3 {
			ts.Fatalf("nodedist release: start the server first and give VERSION [LTS]")
		}
		lts := ""
		if len(args) == 3 {
			lts = args[2]
		}
		f.mu.Lock()
		f.releases[args[1]] = lts
		f.mu.Unlock()
	case "corrupt":
		f, ok := nodeDistServers[key]
		if !ok || len(args) != 2 {
			ts.Fatalf("nodedist corrupt: start the server first and give VERSION")
		}
		f.mu.Lock()
		f.corrupt[args[1]] = true
		f.mu.Unlock()
	default:
		ts.Fatalf("nodedist: unknown command %q", args[0])
	}
}
