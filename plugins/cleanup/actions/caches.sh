#!/usr/bin/env bash
# Action: remove the cache folders of the tools switched on in the setting caches.
# Only folders that exist are touched; the dry run reports each one with its size.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

names=()
paths=()
while IFS= read -r name; do
  [ -n "$name" ] || continue
  path=$(cache_path "$name")
  label=$(cache_label "$name")
  case "$(cache_state "$name" "$path")" in
  unsafe)
    # checked for every cache before anything is removed
    sdk emit log error "$label: $path is never removed; set the folder of the cache or leave the setting empty"
    exit 2
    ;;
  link) sdk emit log warn "$label: $path is a symbolic link, left alone" ;;
  dir)
    names+=("$name")
    paths+=("$path")
    ;;
  *) ;;
  esac
done < <(cache_names)

total=0
count=0
failed=0
for i in "${!names[@]}"; do
  path=${paths[$i]}
  label=$(cache_label "${names[$i]}")
  kb=$(size_kb "$path")
  if ! is_dry_run && ! remove_tree "$path"; then
    sdk emit log error "$label: $path could not be removed completely"
    failed=1
    continue
  fi
  sdk emit change file delete "$path" "$label, $(human_kb "$kb")"
  total=$((total + kb))
  count=$((count + 1))
done

data="{\"bytes\":$((total * 1024)),\"count\":$count}"
if [ "$count" -eq 0 ]; then
  sdk emit result "No caches to clean" "$data"
elif is_dry_run; then
  sdk emit result "Would free $(human_kb "$total") in $count caches" "$data"
else
  sdk emit result "Freed $(human_kb "$total") in $count caches" "$data"
fi
exit "$failed"
