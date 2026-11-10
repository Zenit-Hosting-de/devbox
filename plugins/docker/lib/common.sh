#!/usr/bin/env bash
# Shared functions of the docker plugin. Sourced by checks, actions and consumers.
# Uses only devbox sdk, the commands listed in requires.commands and, where they exist,
# docker, the package manager of the distribution, sudo, systemctl, getent, groupadd
# and usermod.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

# quiet_sdk ARGS…: devbox sdk without protocol messages, for the plugin's own state.
quiet_sdk() { DEVBOX_MSG_FD='' DEVBOX_BACKUP_DIR='' "$DEVBOX_BIN" sdk "$@"; }

# stage_sdk ARGS…: devbox sdk on a working copy: always writes, no messages, no backup.
stage_sdk() { DEVBOX_DRY_RUN=0 DEVBOX_MSG_FD='' DEVBOX_BACKUP_DIR='' "$DEVBOX_BIN" sdk "$@"; }

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# fail TEXT [CODE]: reports an error and ends the script.
fail() {
  sdk emit log error "$1"
  echo "$1" >&2
  exit "${2:-1}"
}

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\t'/\\t}
  s=${s//$'\n'/\\n}
  printf '"%s"' "$s"
}

# json_list ITEMS…: the items as a JSON array of strings.
json_list() {
  local out="" i
  for i in "$@"; do out+="${out:+,}$(json_str "$i")"; done
  printf '[%s]' "$out"
}

# pointer_token TEXT: TEXT as one token of a JSON Pointer.
pointer_token() {
  local s=${1//\~/\~0}
  printf '%s' "${s//\//\~1}"
}

# setting_items POINTER: the values below a setting, one per line, e.g.
# "dockerPackages/*" or "distroPackages/apt/*".
setting_items() {
  sdk json-get "$DEVBOX_REQUEST" "/settings/$1" 2>/dev/null || true
}

# ---------------------------------------------------------------------------------
# Packages
# ---------------------------------------------------------------------------------

# package_manager: apt, dnf, pacman or zypper; nothing when none is found or the
# configured one is not in PATH.
package_manager() {
  local m cmd
  m=$(sdk setting manager)
  if [ "$m" != auto ]; then
    cmd=$m
    [ "$m" != apt ] || cmd=apt-get
    if command -v "$cmd" >/dev/null 2>&1; then
      echo "$m"
    else
      echo "Package manager $m is set, but $cmd is not in PATH" >&2
    fi
    return 0
  fi
  for m in apt dnf pacman zypper; do
    cmd=$m
    [ "$m" != apt ] || cmd=apt-get
    if command -v "$cmd" >/dev/null 2>&1; then
      echo "$m"
      return 0
    fi
  done
}

# shellcheck disable=SC2034 # used by the checks and actions that source this file
no_manager_text="No supported package manager found (apt, dnf, pacman, zypper)"

# wanted_packages MANAGER: the packages of the configured source, one per line.
wanted_packages() {
  if [ "$(sdk setting source)" = docker ]; then
    setting_items 'dockerPackages/*'
  else
    setting_items "distroPackages/$1/*"
  fi
}

# installed_version MANAGER PACKAGE: the installed version; fails when it is not
# installed.
installed_version() {
  local out
  case "$1" in
  apt)
    # shellcheck disable=SC2016 # a format of dpkg-query, not a shell variable
    out=$(dpkg-query -W -f='${Status}\t${Version}' "$2" 2>/dev/null) || return 1
    [[ $out == "install ok installed"$'\t'* ]] || return 1
    printf '%s\n' "${out#*$'\t'}"
    ;;
  dnf | zypper) rpm -q --whatprovides --qf '%{VERSION}-%{RELEASE}\n' "$2" 2>/dev/null ;;
  pacman)
    out=$(pacman -Q "$2" 2>/dev/null) || return 1
    printf '%s\n' "${out#* }"
    ;;
  *) return 1 ;;
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

# ---------------------------------------------------------------------------------
# Files outside the home directory
# ---------------------------------------------------------------------------------

# for_path PATH COMMAND…: runs the command directly when the directory of PATH (or
# its nearest existing parent) is writable, else as root.
for_path() {
  local dir=${1%/*}
  shift
  while [ -n "$dir" ] && [ ! -d "$dir" ]; do dir=${dir%/*}; done
  if [ -w "${dir:-/}" ]; then
    "$@"
  else
    as_root "$@"
  fi
}

# backup FILE: keeps the first original of FILE per session, like devbox sdk does.
backup() {
  local target
  [ -n "${DEVBOX_BACKUP_DIR:-}" ] || return 0
  [ -f "$1" ] || return 0
  target="$DEVBOX_BACKUP_DIR$1"
  [ -e "$target" ] || install -D -m 600 "$1" "$target"
}

# differs SOURCE DEST: whether DEST is missing or has another content than SOURCE.
differs() { [ ! -e "$2" ] || ! cmp -s "$1" "$2"; }

# put_file SOURCE DEST MODE SUMMARY: installs the prepared file SOURCE as DEST: backup
# first, a change message, in a dry run only the message. Call it when they differ.
put_file() {
  local src=$1 dest=$2 mode=$3 op=create
  [ ! -e "$dest" ] || op=update
  if ! is_dry_run; then
    backup "$dest"
    for_path "$dest" install -D -m "$mode" "$src" "$dest"
  fi
  sdk emit change file "$op" "$dest" "$4"
}

# drop_file FILE SUMMARY: deletes FILE with backup and change message.
drop_file() {
  [ -e "$1" ] || return 0
  if ! is_dry_run; then
    backup "$1"
    for_path "$1" rm -f "$1"
  fi
  sdk emit change file delete "$1" "$2"
}

# stage_dir: creates a private working directory in the cache of the plugin, removed
# when the script ends, and prints its path.
STAGE=""
stage_dir() {
  STAGE="$DEVBOX_CACHE_DIR/stage.$$"
  rm -rf "$STAGE"
  mkdir -p "$STAGE"
  chmod 700 "$STAGE"
  trap 'rm -rf "$STAGE"' EXIT
}

# ---------------------------------------------------------------------------------
# Service and group
# ---------------------------------------------------------------------------------

# service_active NAME: whether systemctl exists and reports the service as running.
service_active() {
  command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet "$1" 2>/dev/null
}

# group_user: the user for the group, the current one unless the setting names one.
group_user() {
  local user
  user=$(sdk setting user)
  [ -n "$user" ] || user=$(id -un)
  printf '%s\n' "$user"
}

# in_group USER GROUP: whether the user database lists USER as a member of GROUP.
in_group() {
  local g groups
  groups=$(id -nG "$1" 2>/dev/null) || return 1
  for g in $groups; do
    [ "$g" != "$2" ] || return 0
  done
  return 1
}

# ---------------------------------------------------------------------------------
# daemon.json
# ---------------------------------------------------------------------------------

# The settings that configure writes, as "setting key type".
# shellcheck disable=SC2034 # used by the scripts that source this file
DAEMON_KEYS=(
  "registryMirrors registry-mirrors list"
  "logDriver log-driver text"
  "logOptions log-opts object"
  "defaultAddressPools default-address-pools list"
)

# daemon_wanted SETTING TYPE: the value of the setting as JSON; nothing when the
# setting is empty and the key therefore not managed.
daemon_wanted() {
  local v
  v=$(sdk setting "$1")
  case "$2:$v" in
  text:) ;;
  text:*) json_str "$v" ;;
  list:"[]" | object:"{}") ;;
  *) printf '%s' "$v" ;;
  esac
}

# daemon_current FILE KEY TYPE: the value of KEY in FILE as JSON; fails when the key
# is missing.
daemon_current() {
  local v
  [ -f "$1" ] || return 1
  v=$(sdk json-get "$1" "/$2" 2>/dev/null) || return 1
  if [ "$3" = text ]; then
    json_str "$v"
  else
    printf '%s' "$v"
  fi
}

# ---------------------------------------------------------------------------------
# Registry logins in the client configuration
# ---------------------------------------------------------------------------------

# login_state: file in which the plugin records the logins it wrote, so that removing
# takes exactly them away again.
login_state() { printf '%s\n' "$DEVBOX_STATE_DIR/logins.json"; }

# state_get POINTER: a value of the login state; nothing when it is missing.
state_get() {
  local file
  file=$(login_state)
  [ -f "$file" ] || return 0
  sdk json-get "$file" "$1" 2>/dev/null || true
}

# state_set POINTER JSON / state_drop POINTER: change the login state (not in a dry
# run).
state_set() { quiet_sdk json-set --mode 600 "$(login_state)" "$1" "$2"; }
state_drop() { quiet_sdk json-remove "$(login_state)" "$1"; }

# has_keys FILE POINTER: whether the object at POINTER in FILE has members.
has_keys() {
  local keys
  keys=$(sdk json-keys "$1" "$2" 2>/dev/null) || return 1
  [ -n "$keys" ]
}

# url_host URL: host and port of an address.
url_host() {
  local h=${1#*://}
  h=${h%%/*}
  printf '%s\n' "${h##*@}"
}

# valid_host HOST: a registry host has no blanks, slashes, quotes or backslashes.
valid_host() {
  [[ -n $1 && ! $1 =~ [[:space:]/\"\\] ]]
}

# remove_login NAME HOST FILE: takes the login of the instance NAME for HOST out of the
# client configuration FILE again. Objects and the file itself go as well when the
# plugin created them and they are empty, so the file is as before.
remove_login() {
  local name=$1 host=$2 file=$3 entry dir
  entry="/auths/$(pointer_token "$host")"
  if [ -f "$file" ]; then
    sdk json-remove "$file" "$entry/auth"
    if ! is_dry_run; then
      has_keys "$file" "$entry" || sdk json-remove "$file" "$entry"
      if [ "$(state_get /created/auths)" = true ] && ! has_keys "$file" /auths; then
        sdk json-remove "$file" /auths
        state_drop /created/auths
      fi
      if [ "$(state_get /created/file)" = true ] && ! has_keys "$file" ""; then
        rm -f "$file"
        sdk emit change file delete "$file" "created by devbox, empty after removing the login"
        state_drop /created/file
        dir=$(state_get /created/dir)
        if [ -n "$dir" ]; then
          rmdir "$dir" 2>/dev/null || true
          state_drop /created/dir
        fi
      fi
    fi
  fi
  state_drop "/instances/$name"
  if ! is_dry_run && ! has_keys "$(login_state)" /instances; then
    state_drop /created
  fi
}
