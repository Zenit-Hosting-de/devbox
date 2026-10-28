#!/usr/bin/env bash
# Shared functions of the base plugin. Sourced by checks and actions.
# Uses only devbox sdk and the package manager of the distribution.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# json_list ITEMS…: the items as a JSON array of strings.
json_list() {
  local out="" i
  for i in "$@"; do out+="${out:+,}$(json_str "$i")"; done
  printf '[%s]' "$out"
}

# setting_list KEY: the items of a string array setting, one per line.
setting_list() {
  # echo ends the last item with a newline, so two lists never run together
  { sdk setting "$1" | tr -d '[]"\n ' | tr ',' '\n'; echo; } | sed '/^$/d'
}

# wanted_packages: base and extra packages, without duplicates, in order.
wanted_packages() {
  { setting_list packages; setting_list extraPackages; } | awk '!seen[$0]++'
}

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

# missing_packages MANAGER: wanted packages that are not installed, one per line.
missing_packages() {
  local p
  while IFS= read -r p; do
    is_installed "$1" "$p" || echo "$p"
  done < <(wanted_packages)
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
