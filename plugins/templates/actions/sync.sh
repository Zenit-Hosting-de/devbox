#!/usr/bin/env bash
# Action: fetch every source as a shallow copy, or bring its copy up to date, and remove
# the copies of sources that are no longer configured.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

# Tokens come from the credential helper of Git, never from a prompt in the session.
export GIT_TERMINAL_PROMPT=0

declare -A wanted=()
changed=0
failed=0

for src in "${SOURCES[@]}"; do
  shown=$(show "$src")
  url=$(git_url "$src") || fail 2 "sources: $shown is not a git+https://, git+ssh:// or git+file:// address"
  key=$(short_hash "$src")
  dir="$CACHE_DIR/$key"
  wanted[$key]=1
  sdk emit step "sync-$key" running "$shown"
  if [ -d "$dir/.git" ]; then
    have=$(git -C "$dir" rev-parse HEAD)
    remote=""
    if ! read -r remote _ < <(git ls-remote -- "$url" HEAD) || [ -z "$remote" ]; then
      sdk emit log warn "$shown is not reachable; the copy stays as it is"
      sdk emit step "sync-$key" failed "$shown"
      failed=$((failed + 1))
      continue
    fi
    if [ "$have" = "$remote" ]; then
      sdk emit step "sync-$key" skipped "$shown"
      continue
    fi
    if ! is_dry_run; then
      if ! git -C "$dir" fetch --quiet --depth 1 --no-tags --no-recurse-submodules origin HEAD; then
        sdk emit log warn "$shown could not be fetched; the copy stays as it is"
        sdk emit step "sync-$key" failed "$shown"
        failed=$((failed + 1))
        continue
      fi
      git -C "$dir" reset --quiet --hard FETCH_HEAD
      git -C "$dir" clean --quiet -ffdx
    fi
    sdk emit change file update "$dir" "templates of $shown, ${have:0:7} to ${remote:0:7}"
  else
    if ! is_dry_run; then
      mkdir -p "$CACHE_DIR"
      tmp=$(mktemp -d "$CACHE_DIR/.fetch.XXXXXX")
      if ! git clone --quiet --depth 1 --no-tags --no-recurse-submodules -- "$url" "$tmp/copy"; then
        rm -rf "$tmp"
        sdk emit log warn "$shown could not be fetched"
        sdk emit step "sync-$key" failed "$shown"
        failed=$((failed + 1))
        continue
      fi
      rm -rf "$dir"
      mv "$tmp/copy" "$dir"
      rm -rf "$tmp"
    fi
    sdk emit change file create "$dir" "templates of $shown"
  fi
  changed=$((changed + 1))
  sdk emit step "sync-$key" "done" "$shown"
done

# Copies of sources that are no longer configured.
removed=0
for dir in "$CACHE_DIR"/*/; do
  dir=${dir%/}
  key=${dir##*/}
  [[ $key =~ ^[0-9a-f]{16}$ ]] && [ -d "$dir/.git" ] && [ -z "${wanted[$key]:-}" ] || continue
  is_dry_run || rm -rf "$dir"
  sdk emit change file delete "$dir" "copy of a source that is no longer configured"
  removed=$((removed + 1))
done

if [ "$failed" -gt 0 ]; then
  fail 3 "$failed of ${#SOURCES[@]} sources could not be fetched"
fi
word=sources
[ "${#SOURCES[@]}" -ne 1 ] || word=source
sdk emit result "${#SOURCES[@]} $word, $changed fetched, $removed removed" \
  "{\"sources\":${#SOURCES[@]},\"fetched\":$changed,\"removed\":$removed}"
