#!/usr/bin/env bash
# Action: remove old build folders below the project folders.
#
# A folder is removed only when all of this holds:
#   - it lies below an entry of projectRoots and is a real folder (symbolic links are
#     neither followed nor removed, other file systems are not entered),
#   - its name matches an entry of patterns (and the marker of that entry, if any),
#   - it is no Git repository and Git does not track anything in it,
#   - nothing in it was changed during the last olderThanDays days.
# The dry run reports each folder with its size and removes nothing.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

days=$(sdk setting olderThanDays)
case "$days" in
'' | *[!0-9]* | 0)
  sdk emit log error "olderThanDays must be a whole number of at least 1"
  exit 2
  ;;
esac

# Patterns: NAME or NAME:MARKER. Entries with the same NAME are one find test; the
# folder matches when one of them has no marker or one marker is found next to it.
names=()
always=()
markers=()
declare -A index=()
while IFS= read -r pattern; do
  [ -n "$pattern" ] || continue
  name=${pattern%%:*}
  marker=""
  [ "$name" = "$pattern" ] || marker=${pattern#*:}
  case "$name" in
  '' | . | .. | .git | */*)
    sdk emit log error "patterns: $pattern is not a folder name"
    exit 2
    ;;
  esac
  case "$marker" in
  */* | *:*)
    sdk emit log error "patterns: the marker of $pattern is not a file name"
    exit 2
    ;;
  esac
  if [ "$name" != "$pattern" ] && [ -z "$marker" ]; then
    sdk emit log error "patterns: the marker of $pattern is empty"
    exit 2
  fi
  i=${index[$name]:-}
  if [ -z "$i" ]; then
    i=${#names[@]}
    index[$name]=$i
    names+=("$name")
    always+=(0)
    markers+=("")
  fi
  if [ -z "$marker" ]; then
    always[i]=1
  else
    markers[i]+="$marker"$'\n'
  fi
done < <(setting_items patterns)

# Project folders: absolute, resolved once, never the home directory or above it.
roots=()
while IFS= read -r entry; do
  [ -n "$entry" ] || continue
  case "$entry" in
  /*) ;;
  *)
    sdk emit log error "projectRoots: $entry is not an absolute path"
    exit 2
    ;;
  esac
  if ! root=$(physical_dir "$entry"); then
    sdk emit log info "Project folder $entry does not exist, skipped"
    continue
  fi
  if unsafe_path "$root"; then
    sdk emit log error "projectRoots: $entry is the home directory or a folder above it and is never searched"
    exit 2
  fi
  roots+=("$root")
done < <(setting_items projectRoots)

if [ "${#roots[@]}" -eq 0 ] || [ "${#names[@]}" -eq 0 ]; then
  sdk emit result "No project folders to search" '{"bytes":0,"count":0}'
  exit 0
fi

tests=()
for i in "${!names[@]}"; do
  [ "${#tests[@]}" -eq 0 ] || tests+=(-o)
  tests+=(-name "${names[$i]}" -printf "$i\\t%p\\0")
done

have_git=0
command -v git >/dev/null 2>&1 && have_git=1

# marker_found INDEX PARENT: the pattern has no marker, or PARENT holds an entry that
# matches one of its markers.
marker_found() {
  local m
  [ "${always[$1]}" = 1 ] && return 0
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    [ -n "$(find "$2" -mindepth 1 -maxdepth 1 -name "$m" -print -quit 2>/dev/null)" ] && return 0
  done <<<"${markers[$1]}"
  return 1
}

# in_work_tree DIR: DIR or a folder above it holds a .git entry.
in_work_tree() {
  local d=$1
  while [ -n "$d" ]; do
    [ -e "$d/.git" ] && return 0
    d=${d%/*}
  done
  return 1
}

# git_state PARENT NAME: untracked, tracked or unknown (Git is missing or failed, so
# nothing may be assumed).
git_state() {
  local rc=0
  in_work_tree "$1" || {
    echo untracked
    return 0
  }
  [ "$have_git" = 1 ] || {
    echo unknown
    return 0
  }
  git -C "$1" ls-files --error-unmatch -- ":(literal)$2" >/dev/null 2>&1 || rc=$?
  case "$rc" in
  0) echo tracked ;;
  1) echo untracked ;;
  *) echo unknown ;;
  esac
}

total=0
count=0
failed=0
kept_tracked=0
kept_recent=0
declare -A seen=()
for root in "${roots[@]}"; do
  while IFS= read -r -d '' record; do
    i=${record%%$'\t'*}
    dir=${record#*$'\t'}
    [ -z "${seen[$dir]:-}" ] || continue
    seen[$dir]=1
    parent=${dir%/*}
    marker_found "$i" "$parent" || continue
    if [ -e "$dir/.git" ]; then
      echo "kept $dir: holds a Git repository" >&2
      kept_tracked=$((kept_tracked + 1))
      continue
    fi
    case "$(git_state "$parent" "${dir##*/}")" in
    tracked)
      echo "kept $dir: tracked by Git" >&2
      kept_tracked=$((kept_tracked + 1))
      continue
      ;;
    unknown)
      sdk emit log warn "Kept $dir: it lies in a Git work tree and Git could not tell whether it is tracked"
      kept_tracked=$((kept_tracked + 1))
      continue
      ;;
    esac
    if [ -n "$(find "$dir" -mtime "-$days" -print -quit 2>/dev/null)" ]; then
      echo "kept $dir: changed during the last $days days" >&2
      kept_recent=$((kept_recent + 1))
      continue
    fi
    kb=$(size_kb "$dir")
    if ! is_dry_run && ! remove_tree "$dir"; then
      sdk emit log error "$dir could not be removed completely"
      failed=1
      continue
    fi
    sdk emit change file delete "$dir" "build folder, $(human_kb "$kb")"
    total=$((total + kb))
    count=$((count + 1))
  done < <(find "$root" -mindepth 1 -xdev \( -name .git -prune \) -o \( -type d \( "${tests[@]}" \) -prune \) 2>/dev/null || true)
done

data="{\"bytes\":$((total * 1024)),\"count\":$count,\"keptTracked\":$kept_tracked,\"keptRecent\":$kept_recent}"
if [ "$count" -eq 0 ]; then
  sdk emit result "No build folders to clean" "$data"
elif is_dry_run; then
  sdk emit result "Would free $(human_kb "$total") in $count build folders" "$data"
else
  sdk emit result "Freed $(human_kb "$total") in $count build folders" "$data"
fi
exit "$failed"
