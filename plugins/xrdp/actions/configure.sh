#!/usr/bin/env bash
# shellcheck disable=SC2016 # the block written to the session file contains a literal ${XRDP_SESSION}
# Action: apply the settings to xrdp.ini and sesman.ini and set the session start of
# the desktop. Only the keys listed below are set; every other line of the INI files
# stays byte for byte. The session file gets a marked block.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"
# shellcheck source=../lib/ini.sh
. "$DEVBOX_PLUGIN_DIR/lib/ini.sh"

port=$(sdk setting port)
security=$(sdk setting securityLayer)
bpp=$(sdk setting maxBpp)
root_login=$(sdk setting allowRootLogin)
desktop=$(sdk setting desktop)
xrdp_ini=$(sdk setting xrdpIni)
sesman_ini=$(sdk setting sesmanIni)
session_file=$(sdk setting sessionFile)
session_command=$(sdk setting sessionCommand)

invalid() {
  sdk emit log error "$1"
  exit 2
}
case "$port" in '' | *[!0-9]*) invalid "xrdp.port must be a number from 1 to 65535" ;; esac
if [ "${#port}" -gt 5 ] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
  invalid "xrdp.port must be a number from 1 to 65535"
fi
case "$security" in negotiate | tls | rdp) ;; *) invalid "xrdp.securityLayer must be negotiate, tls or rdp" ;; esac
case "$bpp" in 8 | 15 | 16 | 24 | 32) ;; *) invalid "xrdp.maxBpp must be 8, 15, 16, 24 or 32" ;; esac
case "$root_login" in true | false) ;; *) invalid "xrdp.allowRootLogin must be true or false" ;; esac
case "$desktop" in xfce | none) ;; *) invalid "xrdp.desktop must be xfce or none" ;; esac
[[ $session_command =~ ^[A-Za-z0-9_./-]+(\ [A-Za-z0-9_./=-]+)*$ ]] ||
  invalid "xrdp.sessionCommand must be a command with plain arguments"
for f in "$xrdp_ini" "$sesman_ini" "$session_file"; do
  [ -n "$f" ] || invalid "xrdp.xrdpIni, xrdp.sesmanIni and xrdp.sessionFile must not be empty"
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
ini_changed=0

# same_content A B: both files have the same bytes.
same_content() { [ "$(cat "$1" && printf x)" = "$(cat "$2" && printf x)" ]; }

# edit_ini NAME FILE SPEC…: sets the keys of the specs ("Section|key|value") in FILE.
# One change message per key that differs; the file is written once, after a backup.
edit_ini() {
  local name=$1 file=$2 state spec section key value current count=0
  local orig="$work/$1.orig" new="$work/$1.new" next="$work/$1.next"
  shift 2
  state=$(file_state "$file")
  case "$state" in
  present) cp "$file" "$orig" ;;
  hidden)
    if is_dry_run; then
      sdk emit change file update "$file" "set $# keys where they differ (only root can read the file)"
      return 0
    fi
    load_hidden "$file" "$orig" || state=absent
    ;;
  esac
  if [ "$state" = absent ]; then
    if is_dry_run; then
      # in the dry run of a setup plan xrdp is not installed yet
      sdk emit change file update "$file" "set $# keys once xrdp is installed"
      return 0
    fi
    sdk emit log error "$file does not exist. Install xrdp first (xrdp.install)."
    exit 3
  fi

  cp "$orig" "$new"
  for spec in "$@"; do
    IFS='|' read -r section key value <<<"$spec"
    current=$(ini_get "$new" "$section" "$key") || current="not set"
    ini_set "$new" "$section" "$key" "$value" >"$next"
    if ! same_content "$new" "$next"; then
      sdk emit change file update "$file" "[$section] $key=$value (was $current)"
      count=$((count + 1))
    fi
    mv "$next" "$new"
  done
  [ "$count" -gt 0 ] || return 0
  ini_changed=1
  is_dry_run && return 0
  sdk emit step "ini-$name" running "Write $file"
  backup_from "$orig" "$file"
  replace_file "$new" "$file" 644
  sdk emit step "ini-$name" "done" "Write $file"
}

# only_block FILE: true when FILE holds nothing but the marked block of this plugin.
only_block() {
  local line inside=0 found=0
  [ -f "$1" ] && [ -r "$1" ] || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
    '# >>> devbox:xrdp >>>') inside=1 found=1 ;;
    '# <<< devbox:xrdp <<<') inside=0 ;;
    *)
      [ "$inside" -eq 1 ] || [[ $line =~ ^[[:space:]]*$ ]] || return 1
      ;;
    esac
  done <"$1"
  [ "$found" -eq 1 ]
}

# both INI files must exist before the first one is written
if ! is_dry_run; then
  for f in "$xrdp_ini" "$sesman_ini"; do
    if [ "$(file_state "$f")" = absent ]; then
      sdk emit log error "$f does not exist. Install xrdp first (xrdp.install)."
      exit 3
    fi
  done
fi

edit_ini xrdp "$xrdp_ini" \
  "Globals|port|$port" \
  "Globals|security_layer|$security" \
  "Globals|max_bpp|$bpp"
edit_ini sesman "$sesman_ini" \
  "Security|AllowRootLogin|$root_login"

if [ "$desktop" = xfce ]; then
  {
    printf '# Starts the desktop in remote sessions of xrdp; other logins skip this block.\n'
    printf 'if [ -n "${XRDP_SESSION:-}" ]; then\n'
    printf '  exec %s\n' "$session_command"
    printf 'fi\n'
  } | sdk block "$session_file" xrdp
elif only_block "$session_file"; then
  # an empty session file would end every session at once, so the file goes with the block
  sdk emit change file delete "$session_file" "devbox:xrdp block removed, nothing else in the file"
  if ! is_dry_run; then
    backup_from "$session_file" "$session_file"
    rm -f "$session_file"
  fi
else
  sdk block-remove "$session_file" xrdp
fi

if is_dry_run; then
  sdk emit result "The configuration of xrdp would be applied"
  exit 0
fi
if [ "$ini_changed" -eq 1 ]; then
  sdk emit log info "A restart of xrdp applies the new values (xrdp.restart)."
  sdk emit result "xrdp is configured" '{"restartNeeded":true}'
else
  sdk emit result "xrdp is configured" '{"restartNeeded":false}'
fi
