#!/usr/bin/env bash
# Check: size of the caches that the action caches would remove. Warns above the
# threshold of the setting warnAboveMb.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

warn_mb=$(sdk setting warnAboveMb)
items=""
details=""
total=0
count=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  path=$(cache_path "$name")
  label=$(cache_label "$name")
  case "$(cache_state "$name" "$path")" in
  unsafe)
    printf '{"status":"error","summary":%s,"hints":[{"text":"Set the folder of this cache to a folder below the home directory or leave it empty"}]}\n' \
      "$(json_str "$label: $path is never removed")"
    exit 0
    ;;
  link)
    echo "$label: $path is a symbolic link, left alone" >&2
    continue
    ;;
  dir) ;;
  *) continue ;;
  esac
  kb=$(size_kb "$path")
  total=$((total + kb))
  count=$((count + 1))
  items+="${items:+,}{\"name\":$(json_str "$name"),\"path\":$(json_str "$path"),\"bytes\":$((kb * 1024))}"
  details+="${details:+,}{\"label\":$(json_str "$label"),\"value\":$(json_str "$(human_kb "$kb")")}"
done < <(cache_names)

data="{\"totalBytes\":$((total * 1024)),\"items\":[$items]}"
if [ "$count" -eq 0 ]; then
  printf '{"status":"ok","summary":"No caches found","data":%s}\n' "$data"
elif [ "$warn_mb" -gt 0 ] && [ "$total" -gt $((warn_mb * 1024)) ]; then
  printf '{"status":"warn","summary":%s,"data":%s,"details":[%s],"hints":[{"text":"Clean the caches","action":"cleanup.caches"}]}\n' \
    "$(json_str "Caches use $(human_kb "$total"), more than $(human_kb $((warn_mb * 1024)))")" "$data" "$details"
else
  printf '{"status":"ok","summary":%s,"data":%s,"details":[%s]}\n' \
    "$(json_str "Caches use $(human_kb "$total")")" "$data" "$details"
fi
