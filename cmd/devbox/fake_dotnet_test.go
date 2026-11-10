package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeDotnet serves the part of the .NET release infrastructure that the dotnet plugin
// uses: /dotnet/release-metadata/releases-index.json, the releases.json of every
// channel next to it and /dotnet-install.sh. The install script is a stand-in that
// creates the directory layout of an SDK with a fake dotnet instead of downloading.
type fakeDotnet struct {
	mu       sync.Mutex
	channels map[string]dotnetChannel // "10.0" -> release type and support phase
	sdks     map[string][]string      // channel -> SDK versions
	broken   map[string]bool          // SDK versions the install script fails for
	srv      *httptest.Server
}

type dotnetChannel struct{ releaseType, phase string }

// fakeDotnetMuxer is a stand-in for the dotnet executable. It knows --version,
// --list-sdks and "workload install IDS…", which records the workloads like the
// file-based installer of the SDK (metadata/workloads/BAND/InstalledWorkloads/ID).
const fakeDotnetMuxer = `#!/bin/sh
root=$(cd "$(dirname "$0")" && pwd)
newest=$(ls "$root/sdk" | sort -V | tail -n1)
case "$1" in
--version) echo "$newest" ;;
--list-sdks) for v in $(ls "$root/sdk" | sort -V); do echo "$v [$root/sdk]"; done ;;
workload)
	[ "$2" = install ] || { echo "fake dotnet: only workload install is supported" >&2; exit 1; }
	shift 2
	b=${newest##*.}
	band=${newest%.*}.$((b / 100))00
	mkdir -p "$root/metadata/workloads/$band/InstalledWorkloads"
	for id in "$@"; do
		: >"$root/metadata/workloads/$band/InstalledWorkloads/$id"
		echo "workload install $id ($newest)" >>"$root/workload.log"
	done
	;;
*) echo "fake dotnet: unsupported: $*" >&2; exit 1 ;;
esac
`

// fakeDotnetInstall is a stand-in for dotnet-install.sh. It requires the options the
// plugin passes, logs them and creates sdk/VERSION, the shared runtimes (runtime
// version MAJOR.MINOR.(PATCH mod 100)) and the dotnet executable. BROKEN lists the
// versions it fails for.
const fakeDotnetInstall = `#!/bin/sh
version= dir= arch= feed= nopath=
while [ $# -gt 0 ]; do
	case "$1" in
	--version) version=$2; shift 2 ;;
	--install-dir) dir=$2; shift 2 ;;
	--architecture) arch=$2; shift 2 ;;
	--azure-feed) feed=$2; shift 2 ;;
	--no-path) nopath=1; shift ;;
	*) echo "fake dotnet-install.sh: unknown option $1" >&2; exit 1 ;;
	esac
done
if [ -z "$version" ] || [ -z "$dir" ] || [ -z "$arch" ] || [ -z "$feed" ] || [ -z "$nopath" ]; then
	echo "fake dotnet-install.sh: missing options" >&2
	exit 1
fi
case " BROKEN " in *" $version "*) echo "fake dotnet-install.sh: download of $version failed" >&2; exit 1 ;; esac
mm=${version%.*}
patch=${version##*.}
rt=$mm.$((patch % 100))
mkdir -p "$dir/sdk/$version" "$dir/shared/Microsoft.NETCore.App/$rt" "$dir/shared/Microsoft.AspNetCore.App/$rt" "$dir/host/fxr/$rt"
printf '{"runtimeOptions":{"tfm":"net%s","framework":{"name":"Microsoft.NETCore.App","version":"%s"}}}\n' "$mm" "$rt" >"$dir/sdk/$version/dotnet.runtimeconfig.json"
cat >"$dir/dotnet" <<'MUXER'
MUXER_BODY
MUXER
chmod +x "$dir/dotnet"
echo "--version $version --architecture $arch --azure-feed $feed" >>"$dir/install.log"
`

func dotnetVersionLess(a, b string) bool {
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

func dotnetRuntime(sdk string) string {
	var major, minor, patch int
	_, _ = fmt.Sscanf(sdk, "%d.%d.%d", &major, &minor, &patch)
	return fmt.Sprintf("%d.%d.%d", major, minor, patch%100)
}

// index returns releases-index.json: one entry per channel, newest channel first.
func (f *fakeDotnet) index(base string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	channels := make([]string, 0, len(f.channels))
	for c := range f.channels {
		channels = append(channels, c)
	}
	sort.Slice(channels, func(i, j int) bool { return dotnetVersionLess(channels[j]+".0", channels[i]+".0") })
	out := make([]map[string]any, 0, len(channels))
	for _, c := range channels {
		sdks := append([]string(nil), f.sdks[c]...)
		if len(sdks) == 0 {
			continue
		}
		sort.Slice(sdks, func(i, j int) bool { return dotnetVersionLess(sdks[i], sdks[j]) })
		latest := sdks[len(sdks)-1]
		out = append(out, map[string]any{
			"channel-version": c, "latest-release": dotnetRuntime(latest), "latest-release-date": "2026-01-01",
			"security": false, "latest-runtime": dotnetRuntime(latest), "latest-sdk": latest, "product": ".NET",
			"support-phase": f.channels[c].phase, "release-type": f.channels[c].releaseType,
			"releases.json": base + "/" + c + "/releases.json",
		})
	}
	return map[string]any{"releases-index": out}
}

// releases returns the releases.json of a channel: one release per SDK.
func (f *fakeDotnet) releases(channel string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.channels[channel]; !ok {
		return nil, false
	}
	rels := make([]map[string]any, 0, len(f.sdks[channel]))
	for _, v := range f.sdks[channel] {
		rels = append(rels, map[string]any{
			"release-version": dotnetRuntime(v),
			"sdk":             map[string]any{"version": v},
			"sdks":            []map[string]any{{"version": v}},
		})
	}
	return map[string]any{"channel-version": channel, "releases": rels}, true
}

func (f *fakeDotnet) script() string {
	f.mu.Lock()
	broken := make([]string, 0, len(f.broken))
	for v := range f.broken {
		broken = append(broken, v)
	}
	f.mu.Unlock()
	s := strings.Replace(fakeDotnetInstall, "MUXER_BODY\n", fakeDotnetMuxer, 1)
	return strings.Replace(s, "BROKEN", strings.Join(broken, " "), 1)
}

func (f *fakeDotnet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const meta = "/dotnet/release-metadata/"
	path := r.URL.Path
	switch {
	case path == "/dotnet-install.sh":
		_, _ = w.Write([]byte(f.script()))
	case path == meta+"releases-index.json":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.index(f.srv.URL + strings.TrimSuffix(meta, "/")))
	case strings.HasPrefix(path, meta) && strings.HasSuffix(path, "/releases.json"):
		rel, ok := f.releases(strings.TrimSuffix(strings.TrimPrefix(path, meta), "/releases.json"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	default:
		http.NotFound(w, r)
	}
}

var (
	dotnetMu      sync.Mutex
	dotnetServers = map[string]*fakeDotnet{}
)

// cmdDotnet is the testscript command
//
//	dotnetfeed start                       start a server, set $DOTNETMETA (releases-index.json),
//	                                       $DOTNETSCRIPT (dotnet-install.sh) and $DOTNETFEED
//	dotnetfeed channel CHANNEL TYPE PHASE  add a channel, e.g. 10.0 lts active
//	dotnetfeed sdk VERSION                 serve an SDK in its channel, e.g. 10.0.100
//	dotnetfeed broken VERSION              let the install script fail for VERSION
func cmdDotnet(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: dotnetfeed start | channel CHANNEL TYPE PHASE | sdk VERSION | broken VERSION")
	}
	key := ts.Getenv("WORK")
	dotnetMu.Lock()
	defer dotnetMu.Unlock()
	if args[0] == "start" {
		f := &fakeDotnet{channels: map[string]dotnetChannel{}, sdks: map[string][]string{}, broken: map[string]bool{}}
		f.srv = httptest.NewServer(f)
		dotnetServers[key] = f
		ts.Defer(f.srv.Close)
		ts.Setenv("DOTNETMETA", f.srv.URL+"/dotnet/release-metadata/releases-index.json")
		ts.Setenv("DOTNETSCRIPT", f.srv.URL+"/dotnet-install.sh")
		ts.Setenv("DOTNETFEED", f.srv.URL+"/dotnet")
		return
	}
	f, ok := dotnetServers[key]
	if !ok {
		ts.Fatalf("dotnetfeed %s: start the server first", args[0])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case args[0] == "channel" && len(args) == 4:
		f.channels[args[1]] = dotnetChannel{releaseType: args[2], phase: args[3]}
	case args[0] == "sdk" && len(args) == 2 && strings.Count(args[1], ".") >= 2:
		parts := strings.SplitN(args[1], ".", 3)
		channel := parts[0] + "." + parts[1]
		if _, ok := f.channels[channel]; !ok {
			ts.Fatalf("dotnetfeed sdk: add channel %s first", channel)
		}
		f.sdks[channel] = append(f.sdks[channel], args[1])
	case args[0] == "broken" && len(args) == 2:
		f.broken[args[1]] = true
	default:
		ts.Fatalf("dotnetfeed: unknown command or wrong arguments: %q", args)
	}
}
