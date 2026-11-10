#!/usr/bin/env bash
# Shared functions of the cleanup plugin. Sourced by the check and the actions.
# Uses only devbox sdk and the commands listed in requires.commands; git and docker are
# used when they are in PATH and left out otherwise.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# setting_items KEY: the items of an array setting, one per line; nothing for an empty
# list.
setting_items() {
  sdk json-get "$DEVBOX_REQUEST" "/settings/$1/*" 2>/dev/null || true
}

# physical_dir DIR: DIR with every symbolic link resolved; fails when it is no folder.
physical_dir() { (cd -P -- "$1" 2>/dev/null && pwd -P); }

# covers_home PATH: PATH is the home directory or a folder above it.
covers_home() {
  local p=$1 home
  for home in "${HOME:-}" "$(physical_dir "${HOME:-/}" || true)"; do
    while [ "${home%/}" != "$home" ]; do home=${home%/}; done
    [ -n "$home" ] || continue
    case "$home/" in "$p"/*) return 0 ;; esac
  done
  return 1
}

# unsafe_path PATH: PATH must never be searched or removed: it is not absolute, holds
# "." or ".." parts, is the file system root, the home directory or a folder above it.
unsafe_path() {
  local p=$1
  case "$p" in /*) ;; *) return 0 ;; esac
  case "$p/" in */../* | */./*) return 0 ;; esac
  while [ "${p%/}" != "$p" ]; do p=${p%/}; done
  [ -n "$p" ] || return 0
  covers_home "$p"
}

# unsafe_cache PATH: like unsafe_path; a cache also lies at least two folders deep.
unsafe_cache() {
  local p=$1
  unsafe_path "$p" && return 0
  while [ "${p%/}" != "$p" ]; do p=${p%/}; done
  p=${p#/}
  case "$p" in */*) return 1 ;; esac
  return 0
}

# size_kb PATH: disk usage of PATH in kibibytes, without following symbolic links.
size_kb() {
  local out
  out=$(du -sk -- "$1" 2>/dev/null) || true
  out=${out%%[!0-9]*}
  echo "${out:-0}"
}

# human_kb KB: a size for people (512 kB, 12.3 MB, 4.5 GB).
human_kb() {
  local kb=$1 v
  if [ "$kb" -lt 1024 ]; then
    echo "$kb kB"
  elif [ "$kb" -lt 1048576 ]; then
    v=$((kb * 10 / 1024))
    echo "$((v / 10)).$((v % 10)) MB"
  else
    v=$((kb * 10 / 1048576))
    echo "$((v / 10)).$((v % 10)) GB"
  fi
}

# remove_tree DIR: deletes DIR without following links or crossing file systems.
remove_tree() { rm -rf --one-file-system -- "${1:?}"; }

# cache_names: the caches switched on in the setting caches, one per line.
cache_names() { setting_items caches; }

# cache_label NAME: the name of a cache for messages.
cache_label() {
  case "$1" in
  npm) echo "npm cache" ;;
  maven) echo "Maven local repository" ;;
  gradle) echo "Gradle cache" ;;
  nuget) echo "NuGet package folder" ;;
  pip) echo "pip cache" ;;
  uv) echo "uv cache" ;;
  *) echo "$1 cache" ;;
  esac
}

# cache_path NAME: the folder of a cache from the setting <name>Cache, without a
# trailing slash; empty when the setting is empty.
cache_path() {
  local p
  p=$(sdk setting "${1}Cache")
  while [ "${#p}" -gt 1 ] && [ "${p%/}" != "$p" ]; do p=${p%/}; done
  echo "$p"
}

# cache_state NAME PATH: unset (no path), unsafe (a path that is never removed), link
# (a symbolic link, left alone), absent (no such folder) or dir.
cache_state() {
  local p=$2
  if [ -z "$p" ]; then
    echo unset
  elif unsafe_cache "$p"; then
    echo unsafe
  elif [ -L "$p" ]; then
    echo link
  elif [ -d "$p" ]; then
    echo dir
  else
    echo absent
  fi
}
