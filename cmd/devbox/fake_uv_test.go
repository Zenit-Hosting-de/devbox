package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeUvScript is a stand-in for uv. It keeps its state in files: the releases it
// can "download" come from @STATE@ (pythons: one Python version per line, uv-version:
// the newest uv, tools/NAME: the newest release of a tool), installations go to the
// directories uv takes from UV_PYTHON_INSTALL_DIR, UV_PYTHON_BIN_DIR, UV_TOOL_DIR and
// UV_TOOL_BIN_DIR. Every changing call is logged to @STATE@/uv.log.
const fakeUvScript = `#!/bin/sh
set -eu
VERSION=@VERSION@
STATE='@STATE@'
BIN=${UV_PYTHON_BIN_DIR:-$HOME/.local/bin}
PYDIR=${UV_PYTHON_INSTALL_DIR:-$HOME/.local/share/uv/python}
TOOLDIR=${UV_TOOL_DIR:-$HOME/.local/share/uv/tools}
TOOLBIN=${UV_TOOL_BIN_DIR:-$HOME/.local/bin}

log() { echo "$*" >>"$STATE/uv.log"; }
minor() { echo "$1" | cut -d. -f1-2; }
matches() { [ "$2" = "$1" ] || case "$2" in "$1".*) true ;; *) false ;; esac; }
installed() {
	[ -d "$PYDIR" ] || return 0
	for d in "$PYDIR"/cpython-*-fake; do
		[ -d "$d" ] || continue
		d=${d##*/cpython-}
		echo "${d%-fake}"
	done | sort -rV
}
available() { [ -f "$STATE/pythons" ] && sort -rV -u "$STATE/pythons" || true; }
newest_installed() { for v in $(installed); do if matches "$1" "$v"; then echo "$v"; return 0; fi; done; return 1; }
newest_available() { for v in $(available); do if [ -z "$1" ] || matches "$1" "$v"; then echo "$v"; return 0; fi; done; return 1; }
pydir() { echo "$PYDIR/cpython-$1-fake"; }
install_one() {
	d=$(pydir "$1")
	mkdir -p "$d/bin" "$BIN"
	printf '#!/bin/sh\necho "Python %s"\n' "$1" >"$d/bin/python3"
	chmod 755 "$d/bin/python3"
	ln -sfn "$d/bin/python3" "$BIN/python$(minor "$1")"
	echo "Installed Python $1" >&2
}
set_default() {
	ln -sfn "$(pydir "$1")/bin/python3" "$BIN/python"
	ln -sfn "$(pydir "$1")/bin/python3" "$BIN/python3"
}
default_version() {
	[ -L "$BIN/python" ] || return 0
	t=$(readlink "$BIN/python")
	t=${t%-fake/bin/python3}
	echo "${t##*/cpython-}"
}
json_entry() { # VERSION PATH
	p=null
	[ -n "$2" ] && p="\"$2\""
	printf '{"key":"cpython-%s-linux-fake","version":"%s","path":%s,"implementation":"cpython","variant":"default"}' "$1" "$1" "$p"
}
tool_latest() { cat "$STATE/tools/$1" 2>/dev/null; }
tool_name() { echo "$1" | sed 's/[^A-Za-z0-9._-].*//' | tr 'A-Z_.' 'a-z--'; }

python_cmd() {
	sub=$1
	shift
	case "$sub" in
	install)
		default=false upgrade=false targets=""
		for a in "$@"; do
			case "$a" in
			--default) default=true ;;
			--upgrade | -U) upgrade=true ;;
			-*) ;;
			*) targets="$targets $a" ;;
			esac
		done
		log "python install $* mirror=${UV_PYTHON_INSTALL_MIRROR:-}"
		[ -n "$targets" ] || targets=$(newest_available "") || { echo "error: no Python download" >&2; exit 2; }
		for t in $targets; do
			if [ "$upgrade" = false ] && v=$(newest_installed "$t"); then
				echo "Python $t is already installed" >&2
			else
				v=$(newest_available "$t") || { echo "error: No download found for request: $t" >&2; exit 2; }
				install_one "$v"
			fi
			if [ "$default" = true ]; then set_default "$v"; fi
		done
		;;
	upgrade)
		log "python upgrade $*"
		old_default=$(default_version)
		for m in $(installed | while read -r v; do minor "$v"; done | sort -u); do
			have=$(newest_installed "$m")
			new=$(newest_available "$m") || continue
			[ "$new" = "$have" ] && continue
			[ "$(printf '%s\n%s\n' "$have" "$new" | sort -V | tail -n1)" = "$new" ] || continue
			install_one "$new"
			if [ -n "$old_default" ] && [ "$(minor "$old_default")" = "$m" ]; then set_default "$new"; fi
		done
		;;
	list)
		only=""
		for a in "$@"; do
			case "$a" in
			--only-installed) only=installed ;;
			--only-downloads) only=downloads ;;
			esac
		done
		sep="" out="["
		if [ "$only" != downloads ]; then
			for v in $(installed); do
				out="$out$sep$(json_entry "$v" "$(pydir "$v")/bin/python3")"
				sep=,
			done
		fi
		if [ "$only" != installed ]; then
			for m in $(available | while read -r v; do minor "$v"; done | sort -u -rV); do
				v=$(newest_available "$m")
				out="$out$sep$(json_entry "$v" "")"
				sep=,
			done
		fi
		echo "$out]"
		;;
	find)
		v=$(newest_installed "${1:-}") || { echo "error: No interpreter found" >&2; exit 2; }
		echo "$(pydir "$v")/bin/python3"
		;;
	uninstall)
		log "python uninstall $*"
		for t in "$@"; do
			for v in $(installed); do
				matches "$t" "$v" || continue
				d=$(pydir "$v")
				for l in "$BIN"/python*; do
					[ -L "$l" ] || continue
					case "$(readlink "$l")" in "$d"/*) rm -f "$l" ;; esac
				done
				rm -rf "$d"
				echo "Uninstalled Python $v" >&2
			done
		done
		;;
	*) echo "fake uv: unsupported: python $sub" >&2; exit 2 ;;
	esac
}

tool_cmd() {
	sub=$1
	shift
	case "$sub" in
	install)
		spec=$1
		name=$(tool_name "$spec")
		log "tool install $spec"
		if [ -f "$TOOLDIR/$name/version" ]; then
			echo "$name is already installed" >&2
			exit 0
		fi
		latest=$(tool_latest "$name") || { echo "error: no package $name" >&2; exit 2; }
		mkdir -p "$TOOLDIR/$name" "$TOOLBIN"
		echo "$latest" >"$TOOLDIR/$name/version"
		printf '#!/bin/sh\necho "%s %s"\n' "$name" "$latest" >"$TOOLBIN/$name"
		chmod 755 "$TOOLBIN/$name"
		;;
	upgrade)
		log "tool upgrade $*"
		[ -d "$TOOLDIR" ] || exit 0
		for d in "$TOOLDIR"/*/; do
			[ -f "$d/version" ] || continue
			name=$(basename "$d")
			latest=$(tool_latest "$name") || continue
			if [ "$(cat "$d/version")" != "$latest" ]; then
				echo "Updated $name v$(cat "$d/version") -> v$latest" >&2
				echo "$latest" >"$d/version"
			fi
		done
		;;
	list)
		outdated=false
		[ "${1:-}" = --outdated ] && outdated=true
		[ -d "$TOOLDIR" ] || { echo "No tools installed" >&2; exit 0; }
		for d in "$TOOLDIR"/*/; do
			[ -f "$d/version" ] || continue
			name=$(basename "$d")
			have=$(cat "$d/version")
			latest=$(tool_latest "$name" || echo "$have")
			if [ "$outdated" = true ]; then
				[ "$latest" != "$have" ] || continue
				echo "$name v$have [latest: $latest]"
			else
				echo "$name v$have"
			fi
			echo "- $name"
		done
		;;
	*) echo "fake uv: unsupported: tool $sub" >&2; exit 2 ;;
	esac
}

self_cmd() {
	[ "${1:-}" = update ] || { echo "fake uv: unsupported: self $*" >&2; exit 2; }
	new=$(cat "$STATE/uv-version")
	if [ "$new" = "$VERSION" ]; then
		echo "success: You're on the latest version of uv (v$VERSION)" >&2
	elif [ "${2:-}" = --dry-run ]; then
		echo "Would update uv from v$VERSION to v$new" >&2
	else
		log "self update $VERSION -> $new"
		sed "s/^VERSION=.*/VERSION=$new/" "$0" >"$0.new"
		chmod 755 "$0.new"
		mv "$0.new" "$0"
		echo "success: Upgraded uv from v$VERSION to v$new!" >&2
	fi
}

case "${1:-}" in
--version) echo "uv $VERSION (fake)" ;;
python) shift; python_cmd "$@" ;;
tool) shift; tool_cmd "$@" ;;
self) shift; self_cmd "$@" ;;
*) echo "fake uv: unsupported: $*" >&2; exit 2 ;;
esac
`

// fakeUvInstaller is the install script the fake server hands out (layout of
// astral.sh/uv/install.sh): it insists on UV_INSTALL_DIR and UV_NO_MODIFY_PATH and
// drops the fake uv there.
const fakeUvInstaller = `#!/bin/sh
set -eu
[ -n "${UV_INSTALL_DIR:-}" ] || { echo "fake installer: UV_INSTALL_DIR is not set" >&2; exit 1; }
[ "${UV_NO_MODIFY_PATH:-}" = 1 ] || { echo "fake installer: UV_NO_MODIFY_PATH is not 1" >&2; exit 1; }
mkdir -p "$UV_INSTALL_DIR"
cat >"$UV_INSTALL_DIR/uv" <<'FAKE_UV_EOF'
@UV@FAKE_UV_EOF
chmod 755 "$UV_INSTALL_DIR/uv"
echo "installer $UV_INSTALL_DIR" >>'@STATE@/uv.log'
echo "installed uv @VERSION@"
`

// fakeUv serves /uv/install.sh; the fake uv reads the rest of its world from dir.
type fakeUv struct {
	dir string
	srv *httptest.Server
}

func (f *fakeUv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/uv/install.sh" {
		http.NotFound(w, r)
		return
	}
	version, err := os.ReadFile(filepath.Join(f.dir, "uv-version")) //nolint:gosec // test state
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := strings.TrimSpace(string(version))
	uv := strings.NewReplacer("@STATE@", f.dir, "@VERSION@", v).Replace(fakeUvScript)
	script := strings.NewReplacer("@UV@", uv, "@STATE@", f.dir, "@VERSION@", v).Replace(fakeUvInstaller)
	w.Header().Set("Content-Type", "text/x-shellscript")
	_, _ = w.Write([]byte(script))
}

var (
	uvDistMu      sync.Mutex
	uvDistServers = map[string]*fakeUv{}
)

// cmdUvDist is the testscript command
//
//	uvdist start                start the server; $UVINSTALL is the installer URL,
//	                            $UVMIRROR a Python mirror URL, $UVSTATE the state
//	                            directory with uv.log
//	uvdist uv VERSION           the newest uv (what the installer and self update give)
//	uvdist python VERSION…      Python releases uv can download
//	uvdist tool NAME VERSION    the newest release of a tool
func cmdUvDist(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: uvdist start | uv VERSION | python VERSION… | tool NAME VERSION")
	}
	key := ts.Getenv("WORK")
	uvDistMu.Lock()
	defer uvDistMu.Unlock()
	f, ok := uvDistServers[key]
	if args[0] != "start" && !ok {
		ts.Fatalf("uvdist %s: start the server first", args[0])
	}
	write := func(name, data string, appendTo bool) {
		p := filepath.Join(f.dir, name)
		ts.Check(os.MkdirAll(filepath.Dir(p), 0o750))
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appendTo {
			flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		fh, err := os.OpenFile(p, flag, 0o600) //nolint:gosec // test state
		ts.Check(err)
		_, err = fh.WriteString(data)
		ts.Check(err)
		ts.Check(fh.Close())
	}
	switch args[0] {
	case "start":
		f = &fakeUv{dir: filepath.Join(key, ".uvdist")}
		ts.Check(os.MkdirAll(filepath.Join(f.dir, "tools"), 0o750))
		f.srv = httptest.NewServer(f)
		uvDistServers[key] = f
		ts.Defer(f.srv.Close)
		write("uv-version", "0.5.0\n", false)
		write("pythons", "", false)
		ts.Setenv("UVINSTALL", f.srv.URL+"/uv/install.sh")
		ts.Setenv("UVMIRROR", f.srv.URL+"/python-build-standalone")
		ts.Setenv("UVSTATE", f.dir)
	case "uv":
		if len(args) != 2 {
			ts.Fatalf("uvdist uv VERSION")
		}
		write("uv-version", args[1]+"\n", false)
	case "python":
		if len(args) < 2 {
			ts.Fatalf("uvdist python VERSION…")
		}
		write("pythons", strings.Join(args[1:], "\n")+"\n", true)
	case "tool":
		if len(args) != 3 {
			ts.Fatalf("uvdist tool NAME VERSION")
		}
		write(filepath.Join("tools", args[1]), args[2]+"\n", false)
	default:
		ts.Fatalf("uvdist: unknown command %q", args[0])
	}
}
