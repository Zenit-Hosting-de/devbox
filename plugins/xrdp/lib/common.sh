#!/usr/bin/env bash
# Shared functions of the xrdp plugin. Sourced by checks and actions.
# Uses only devbox sdk, the commands listed in requires.commands, the package manager
# of the distribution and sudo.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\t'/\\t}
  s=${s//$'\r'/\\r}
  s=${s//$'\n'/\\n}
  printf '"%s"' "$s"
}

# json_list ITEMS…: the items as a JSON array of strings.
json_list() {
  local out="" i
  for i in "$@"; do out+="${out:+,}$(json_str "$i")"; done
  printf '[%s]' "$out"
}

# setting_items KEY: the items of an array setting, one per line.
setting_items() { sdk json-get "$DEVBOX_REQUEST" "/settings/$1/*" 2>/dev/null || true; }

# manager_items KEY MANAGER: the items a setting lists for a package manager (the
# setting is an object with one list per manager), one per line.
manager_items() { sdk json-get "$DEVBOX_REQUEST" "/settings/$1/$2/*" 2>/dev/null || true; }

# package_manager: apt, dnf, pacman or zypper; nothing when none is found or the
# configured one is not in PATH.
package_manager() {
  local m cmd
  m=$(sdk setting manager)
  if [ "$m" != auto ]; then
    cmd=$m
    [ "$m" = apt ] && cmd=apt-get
    if command -v "$cmd" >/dev/null 2>&1; then
      echo "$m"
    else
      echo "Package manager $m is set, but $cmd is not in PATH" >&2
    fi
    return 0
  fi
  command -v apt-get >/dev/null 2>&1 && { echo apt; return 0; }
  command -v dnf >/dev/null 2>&1 && { echo dnf; return 0; }
  command -v pacman >/dev/null 2>&1 && { echo pacman; return 0; }
  command -v zypper >/dev/null 2>&1 && { echo zypper; return 0; }
  return 0
}

# is_installed MANAGER PACKAGE
is_installed() {
  case "$1" in
  apt) dpkg-query -W -f='${Status}' "$2" 2>/dev/null | grep -q 'install ok installed' ;;
  dnf | zypper) rpm -q --whatprovides "$2" >/dev/null 2>&1 ;;
  pacman) pacman -Qq "$2" >/dev/null 2>&1 || pacman -Qqg "$2" >/dev/null 2>&1 ;;
  *) return 1 ;;
  esac
}

# server_packages MANAGER: the packages of xrdp, plus those of the desktop when one is
# chosen, without duplicates, one per line.
server_packages() {
  {
    manager_items packages "$1"
    [ "$(sdk setting desktop)" = none ] || manager_items desktopPackages "$1"
  } | awk 'NF && !seen[$0]++'
}

# missing_of MANAGER PACKAGE…: the packages that are not installed, one per line.
missing_of() {
  local pm=$1 p
  shift
  for p in "$@"; do
    is_installed "$pm" "$p" || echo "$p"
  done
}

# install_packages MANAGER PACKAGE…: installs the packages as root.
install_packages() {
  local pm=$1
  shift
  case "$pm" in
  apt)
    as_root apt-get update
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
    ;;
  dnf) as_root dnf install -y "$@" ;;
  pacman) as_root pacman -S --needed --noconfirm "$@" ;;
  zypper) as_root zypper --non-interactive install "$@" ;;
  esac
}

# as_root COMMAND…: runs the command as root, through sudo unless already root.
as_root() {
  if [ "$(id -u)" = 0 ]; then
    "$@"
  else
    sudo "$@"
  fi
}

# no_manager_text: message when no supported package manager exists.
# shellcheck disable=SC2034 # used by the checks and actions that source this file
no_manager_text="No supported package manager found (apt, dnf, pacman, zypper)"

# --- files that may belong to root -------------------------------------------------

# needs_root PATH: true when the user cannot write PATH (or, for a new file, the
# nearest folder that exists).
needs_root() {
  local p=$1
  while [ ! -e "$p" ]; do
    case "$p" in
    */*) p=${p%/*} ;;
    *) p=. ;;
    esac
    [ -n "$p" ] || p=/
  done
  [ ! -w "$p" ]
}

# for_path PATH COMMAND…: runs the command, as root when PATH needs it.
for_path() {
  local p=$1
  shift
  if needs_root "$p"; then
    as_root "$@"
  else
    "$@"
  fi
}

# file_state FILE: present (readable), absent, or hidden (the user can neither read
# the file nor tell whether it exists, e.g. below a folder only root may enter).
file_state() {
  local dir=${1%/*}
  if [ -f "$1" ] && [ -r "$1" ]; then
    echo present
  elif [ -e "$1" ]; then
    echo hidden
  elif [ -d "$dir" ] && { [ ! -x "$dir" ] || [ ! -r "$dir" ]; }; then
    echo hidden
  else
    echo absent
  fi
}

# load_hidden FILE DEST: copies a hidden file to DEST as root. Returns 1 when the file
# does not exist.
load_hidden() {
  as_root test -f "$1" || return 1
  as_root cat "$1" >"$2"
}

# backup_from COPY FILE: keeps COPY, the original content of FILE, in the backup folder
# of this session (same layout as devbox sdk block). The first original is kept.
backup_from() {
  local target
  [ -n "${DEVBOX_BACKUP_DIR:-}" ] || return 0
  target="$DEVBOX_BACKUP_DIR$2"
  [ ! -e "$target" ] || return 0
  mkdir -p "${target%/*}"
  (
    umask 077
    cp "$1" "$target"
  )
}

# replace_file NEW FILE MODE: writes the content of NEW to FILE, as root when needed.
# An existing file keeps its owner and mode; a new one gets MODE.
replace_file() {
  local new=$1 file=$2 mode=$3
  if for_path "$file" test -e "$file"; then
    for_path "$file" cp "$new" "$file"
  else
    for_path "${file%/*}" mkdir -p "${file%/*}"
    for_path "$file" install -m "$mode" "$new" "$file"
  fi
}

# --- systemd -------------------------------------------------------------------------

# unit_exists SERVICE
unit_exists() { systemctl cat "$1" >/dev/null 2>&1; }

# unit_active SERVICE
unit_active() { systemctl is-active --quiet "$1" 2>/dev/null; }

# unit_enabled SERVICE: enabled for the next start of the machine.
unit_enabled() {
  case "$(systemctl is-enabled "$1" 2>/dev/null || true)" in
  enabled | enabled-runtime) return 0 ;;
  *) return 1 ;;
  esac
}

# services: the units of xrdp, one per line.
services() { setting_items services; }
