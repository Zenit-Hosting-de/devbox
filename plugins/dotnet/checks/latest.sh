#!/usr/bin/env bash
# Check: newer SDKs of the installed channels, channels out of support and a newer
# channel than every installed one (needs the network).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -eq 0 ]; then
  printf '{"status":"ok","summary":"No .NET SDK installed"}\n'
  exit 0
fi

index=$(mktemp "$DEVBOX_CACHE_DIR/index.XXXXXX")
trap 'rm -f "$index"' EXIT
if ! fetch_index "$index" 2>/dev/null; then
  printf '{"status":"error","summary":%s}\n' "$(json_str "Release metadata not reachable: $METADATA")"
  exit 0
fi
rows=$(index_rows "$index")

hints=""
newer=""
eol=""
declare -A seen=()
for v in "${versions[@]}"; do
  ch=$(channel_of "$v") || continue
  [ -n "${seen[$ch]:-}" ] && continue
  seen[$ch]=1
  while read -r c latest _ phase; do
    [ "$c" = "$ch" ] || continue
    if [ "$phase" = eol ]; then
      eol+="${eol:+, }$ch"
      hints+="${hints:+,}{\"text\":$(json_str ".NET $ch is out of support"),\"action\":\"dotnet.install\"}"
    fi
    if [ -n "$latest" ] && [ "$latest" != "$v" ] && [ "$(newest "$v" "$latest")" = "$latest" ]; then
      newer+="${newer:+, }$latest"
      hints+="${hints:+,}{\"text\":$(json_str "$latest is available"),\"action\":\"dotnet.update\"}"
    fi
  done <<<"$rows"
done

# a channel newer than every installed one, taken from the channel setting
channel_latest=$(resolve_version "" "$index" || true)
if [ -n "$channel_latest" ] && [ -z "${seen[$(channel_of "$channel_latest")]:-}" ] &&
  [ "$(newest "${versions[0]}" "$channel_latest")" = "$channel_latest" ]; then
  hints+="${hints:+,}{\"text\":$(json_str "New channel $channel_latest ($CHANNEL)"),\"action\":\"dotnet.install\"}"
fi

summary=""
[ -z "$newer" ] || summary="Updates: $newer"
[ -z "$eol" ] || summary+="${summary:+; }Out of support: $eol"
if [ -n "$summary" ]; then
  printf '{"status":"warn","summary":%s,"hints":[%s]}\n' "$(json_str "$summary")" "$hints"
elif [ -n "$hints" ]; then
  printf '{"status":"ok","summary":"Up to date","hints":[%s]}\n' "$hints"
else
  printf '{"status":"ok","summary":"Up to date"}\n'
fi
