#!/usr/bin/env bash
# Shared parts of the bats tests of the xrdp plugin: fixture INI files modelled on the
# stock xrdp.ini and sesman.ini, fake system commands, and a runner for plugin scripts.
# The fixtures are written at run time, so the repository holds no INI files.

# write_xrdp_ini FILE: a shortened stock xrdp.ini. The key "port" occurs in [Globals],
# [Xorg] and [Xvnc], and in comments.
write_xrdp_ini() {
  cat >"$1" <<'EOF'
[Globals]
; xrdp.ini file version number
ini_version=1

; fork a new process for each incoming connection
fork=true

; ports to listen on, number alone means listen on all interfaces
; 0.0.0.0 or :: if ipv6 is configured
; space between multiple occurrences
;
; Examples:
;   port=3389
;   port=unix://./tmp/xrdp.socket
;   port=tcp://.:3389                           127.0.0.1:3389
;   port=tcp://:3389                            *:3389
port=3389

; 'port' above should be connected to with vsock instead of tcp
use_vsock=false

; regulate if the listening socket use socket option tcp_nodelay
tcp_nodelay=true
tcp_keepalive=true

; security layer can be 'tls', 'rdp' or 'negotiate'
; for client compatible layer
security_layer=negotiate

; minimum security level allowed for client for classic RDP encryption
crypt_level=high

; X.509 certificate and private key
certificate=
key_file=

; set SSL protocols
ssl_protocols=TLSv1.2, TLSv1.3
; set TLS cipher suites
#tls_ciphers=HIGH

autorun=

allow_channels=true
allow_multimon=true
bitmap_cache=true
bitmap_compression=true
bulk_compression=true
#hidelogwindow=true
max_bpp=32
new_cursors=true
; fastpath - can be 'input', 'output', 'both', 'none'
use_fastpath=both

; colors used by windows in RGB format
blue=009cb5
grey=dedede
#black=000000

ls_title=My Login Title
ls_top_window_bg_color=009cb5

[Logging]
; Note: Log levels can be any of: core, error, warning, info, debug, or trace
LogFile=xrdp.log
LogLevel=INFO
EnableSyslog=true
#SyslogLevel=INFO

[Channels]
; Channel names not listed here will be blocked by XRDP.
rdpdr=true
rdpsnd=true
drdynvc=true
cliprdr=true
rail=true
xrdpvr=true

; Session types

; Some session types such as Xorg, X11rdp and Xvnc start a display server.
[Xorg]
name=Xorg
lib=libxup.so
username=ask
password=ask
ip=127.0.0.1
port=-1
code=20

[Xvnc]
name=Xvnc
lib=libvnc.so
username=ask
password=ask
ip=127.0.0.1
port=-1
#xserverbpp=24
#delay_ms=2000
EOF
}

# write_sesman_ini FILE: a shortened stock sesman.ini. "param" occurs several times in
# one section, "LogLevel"-like and "Enable…" keys in several.
write_sesman_ini() {
  cat >"$1" <<'EOF'
;; See `man 5 sesman.ini` for details

[Globals]
ListenAddress=127.0.0.1
ListenPort=3350
EnableUserWindowManager=true
; Give in relative path to user's home directory
UserWindowManager=startwm.sh
; Give in full path or relative path to /etc/xrdp
DefaultWindowManager=startwm.sh
; Give in full path or relative path to /etc/xrdp
ReconnectScript=reconnectwm.sh

[Security]
AllowRootLogin=true
MaxLoginRetry=4
TerminalServerUsers=tsusers
TerminalServerAdmins=tsadmins
; When AlwaysGroupCheck=false access will be permitted
; if the group TerminalServerUsers is not defined.
AlwaysGroupCheck=false
; When RestrictOutboundClipboard=all clipboard from the
; server is not pushed to the client.
RestrictOutboundClipboard=none

[Sessions]
;; X11DisplayOffset - x11 display number offset
; Type: integer
; Default: 10
X11DisplayOffset=10

;; MaxSessions - maximum number of connections to an xrdp server
MaxSessions=50

;; KillDisconnected - kill disconnected sessions
KillDisconnected=false

[Logging]
LogFile=xrdp-sesman.log
LogLevel=INFO
EnableSyslog=true

[Xorg]
; Specify the path of non-suid Xorg executable.
param=/usr/lib/xorg/Xorg
; Leave the rest parameters as-is unless you understand what will happen.
param=-config
param=xrdp/xorg.conf
param=-noreset
param=-nolisten
param=tcp

[SessionVariables]
PULSE_SCRIPT=/etc/xrdp/pulse/default.pa
EOF
}

# fake_commands DIR: systemctl, apt-get, dpkg-query and sudo that only touch files
# below DIR. Units and packages exist as files in DIR/units and DIR/pkgs; every call
# of systemctl and apt-get is logged in DIR/calls.log.
fake_commands() {
  local dir=$1
  mkdir -p "$dir/bin" "$dir/units" "$dir/pkgs" "$dir/enabled" "$dir/active"
  cat >"$dir/bin/systemctl" <<'EOF'
#!/bin/sh
dir=$(dirname "$0")/..
case "$1" in
cat) [ -e "$dir/units/$2" ] ;;
is-active)
  shift
  [ "$1" = --quiet ] && shift
  [ -e "$dir/active/$1" ]
  ;;
is-enabled)
  if [ -e "$dir/enabled/$2" ]; then echo enabled; else echo disabled; exit 1; fi
  ;;
enable | disable | restart)
  echo "systemctl $*" >>"$dir/calls.log"
  cmd=$1
  shift
  [ "$1" = --now ] && shift
  for unit; do
    [ -e "$dir/units/$unit" ] || { echo "Unit $unit not found" >&2; exit 5; }
    case "$cmd" in
    enable) : >"$dir/enabled/$unit"; : >"$dir/active/$unit" ;;
    disable) rm -f "$dir/enabled/$unit" "$dir/active/$unit" ;;
    restart) : >"$dir/active/$unit" ;;
    esac
  done
  ;;
*)
  echo "fake systemctl: unexpected $*" >&2
  exit 64
  ;;
esac
EOF
  cat >"$dir/bin/apt-get" <<'EOF'
#!/bin/sh
dir=$(dirname "$0")/..
echo "apt-get $*" >>"$dir/calls.log"
if [ "$1" = install ] && [ "$2" = -y ]; then
  shift 2
  for name; do
    : >"$dir/pkgs/$name"
    # the package xrdp brings the units
    [ "$name" = xrdp ] && : >"$dir/units/xrdp" && : >"$dir/units/xrdp-sesman"
  done
fi
exit 0
EOF
  cat >"$dir/bin/dpkg-query" <<'EOF'
#!/bin/sh
for name; do :; done
if [ -e "$(dirname "$0")/../pkgs/$name" ]; then
  printf 'install ok installed'
  exit 0
fi
exit 1
EOF
  cat >"$dir/bin/sudo" <<'EOF'
#!/bin/sh
# runs only fake commands and file tools, so a test never touches the real system
case "$1" in
apt-get | systemctl | env | cp | install | mkdir | rm | cat | test) exec "$@" ;;
esac
echo "sudo: $1 not allowed in this test" >&2
exit 1
EOF
  chmod 755 "$dir/bin/systemctl" "$dir/bin/apt-get" "$dir/bin/dpkg-query" "$dir/bin/sudo"
}

# write_request KIND NAME DRYRUN: a request file like the runner writes it. Settings
# come from S_* variables, defaults as in plugin.json with paths below the test folder.
write_request() {
  local dry=false
  local actions=${S_POLKIT_ACTIONS:-'["org.freedesktop.color-manager.create-device", "org.freedesktop.NetworkManager.wifi.scan"]'}
  local packages=${S_PACKAGES:-'{ "apt": ["xrdp", "xorgxrdp"], "pacman": [] }'}
  local sound_packages=${S_SOUND_PACKAGES:-'{ "apt": ["pipewire-module-xrdp"] }'}
  [ "$3" != 1 ] || dry=true
  REQ="$BATS_TEST_TMPDIR/request.json"
  cat >"$REQ" <<EOF
{
  "protocol": 1, "kind": "$1", "plugin": "xrdp", "name": "$2", "dryRun": $dry,
  "params": {},
  "settings": {
    "port": ${S_PORT:-3389},
    "securityLayer": "${S_SECURITY:-negotiate}",
    "maxBpp": ${S_BPP:-32},
    "sound": ${S_SOUND:-false},
    "desktop": "${S_DESKTOP:-xfce}",
    "allowRootLogin": ${S_ROOT:-false},
    "polkit": ${S_POLKIT:-true},
    "polkitActions": $actions,
    "polkitGroup": "${S_POLKIT_GROUP:-}",
    "polkitRuleFile": "${S_RULE_FILE:-$ETC/polkit-1/rules.d/45-devbox-xrdp.rules}",
    "xrdpIni": "${S_XRDP_INI:-$ETC/xrdp/xrdp.ini}",
    "sesmanIni": "${S_SESMAN_INI:-$ETC/xrdp/sesman.ini}",
    "sessionFile": "${S_SESSION_FILE:-$HOME/.xsession}",
    "sessionCommand": "${S_SESSION_COMMAND:-startxfce4}",
    "services": ["xrdp-sesman", "xrdp"],
    "packages": $packages,
    "desktopPackages": { "apt": ["xfce4", "dbus-x11"] },
    "soundPackages": $sound_packages,
    "manager": "${S_MANAGER:-apt}"
  },
  "credential": null,
  "host": { "distro": "ubuntu", "distroVersion": "24.04", "arch": "amd64" }
}
EOF
}

# run_script SCRIPT [DRYRUN]: runs a script of the plugin with the protocol environment
# and the fake commands first in PATH. Messages of actions go to $MSGS.
run_script() {
  local kind=action name=${1##*/}
  name=${name%.sh}
  case "$1" in checks/*) kind=check ;; esac
  write_request "$kind" "$name" "${2:-0}"
  PATH="$FAKE/bin:$PATH" DEVBOX_BIN=$(command -v devbox) DEVBOX_PLUGIN_DIR="$PLUGIN" \
    DEVBOX_REQUEST="$REQ" DEVBOX_MSG_FD=3 DEVBOX_DRY_RUN="${2:-0}" \
    DEVBOX_BACKUP_DIR="$BATS_TEST_TMPDIR/backup" \
    "$PLUGIN/$1" 3>>"$MSGS"
}

# changes: number of change messages so far.
changes() { grep -c '"type":"change"' "$MSGS" || true; }

# reset_messages: forget the messages of earlier runs.
reset_messages() { : >"$MSGS"; }

# refute CMD…: fails the test when the command succeeds (a bare "!" would not).
refute() {
  if "$@"; then
    echo "unexpected success: $*" >&2
    return 1
  fi
}
