#!/usr/bin/env bash
# Check: newer patch releases of the installed major versions and a newer major
# version of the channel (needs the network).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -eq 0 ]; then
  printf '{"status":"ok","summary":"No Node.js installed"}\n'
  exit 0
fi

index=$(mktemp "$DEVBOX_CACHE_DIR/index.XXXXXX")
trap 'rm -f "$index"' EXIT
if ! fetch_index "$index" 2>/dev/null; then
  printf '{"status":"error","summary":%s}\n' "$(json_str "Distribution server not reachable: $DIST")"
  exit 0
fi

hints=""
newer=""
declare -A seen=()
for v in "${versions[@]}"; do
  m=$(major "$v")
  [ -n "${seen[$m]:-}" ] && continue
  seen[$m]=1
  latest=$(resolve_version "$m" "$index") || continue
  if [ "$latest" != "$v" ] && [ "$(printf '%s\n%s\n' "$v" "$latest" | sort -V | tail -n1)" = "$latest" ]; then
    newer+="${newer:+, }$latest"
    hints+="${hints:+,}{\"text\":$(json_str "$latest is available"),\"action\":\"node.update\"}"
  fi
done

# a major version newer than every installed one, taken from the channel
channel_latest=$(resolve_version "" "$index" || true)
if [ -n "$channel_latest" ] && [ "$(major "$channel_latest")" -gt "$(major "${versions[0]}")" ]; then
  hints+="${hints:+,}{\"text\":$(json_str "New major version $channel_latest ($CHANNEL)"),\"action\":\"node.install\"}"
fi

if [ -n "$newer" ]; then
  printf '{"status":"warn","summary":%s,"hints":[%s]}\n' "$(json_str "Updates: $newer")" "$hints"
elif [ -n "$hints" ]; then
  printf '{"status":"ok","summary":"Up to date","hints":[%s]}\n' "$hints"
else
  printf '{"status":"ok","summary":"Up to date"}\n'
fi
