#!/usr/bin/env bash
# Check: newer builds of the installed feature releases (needs the network).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

meta=$(mktemp "$DEVBOX_CACHE_DIR/latest.XXXXXX")
trap 'rm -f "$meta"' EXIT
hints=""
newer=""
declare -A seen=()
while IFS= read -r v; do
  [ -n "$v" ] || continue
  f=$(major "$v")
  [ -n "${seen[$f]:-}" ] && continue
  seen[$f]=1
  if ! fetch_latest "$f" "$meta" 2>/dev/null; then
    printf '{"status":"error","summary":%s}\n' "$(json_str "Release API not reachable: $API")"
    exit 0
  fi
  latest=$(meta_version "$meta")
  if [ "$latest" != "$v" ] && [ "$(printf '%s\n%s\n' "$v" "$latest" | sort -V | tail -n1)" = "$latest" ]; then
    newer+="${newer:+, }$latest"
    hints+="${hints:+,}{\"text\":$(json_str "$latest is available"),\"action\":\"java.update\"}"
  fi
done < <(installed_versions)

if [ -n "$newer" ]; then
  printf '{"status":"warn","summary":%s,"hints":[%s]}\n' "$(json_str "Updates: $newer")" "$hints"
else
  printf '{"status":"ok","summary":"Up to date"}\n'
fi
