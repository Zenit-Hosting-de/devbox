#!/usr/bin/env bash
# Check: newer patch releases of the installed minor versions, a newer minor version,
# a newer uv and outdated tools (needs the network for uv and the tools).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if ! has_uv; then
  printf '{"status":"ok","summary":"uv is not installed"}\n'
  exit 0
fi

hints=""
newer_list=""

if [ "$(sdk setting selfUpdate)" = true ]; then
  if out=$("$UV" self update --dry-run 2>&1) && [[ $out == *"Would update"* ]]; then
    newer_list+="${newer_list:+, }uv"
    hints+="${hints:+,}{\"text\":\"A newer uv is available\",\"action\":\"python.update\"}"
  fi
fi

mapfile -t versions < <(installed_versions)
declare -A seen=()
for v in "${versions[@]}"; do
  m=$(minor "$v")
  [ -n "${seen[$m]:-}" ] && continue
  seen[$m]=1
  latest=$(available_match "$m") || continue
  if newer "$latest" "$v"; then
    newer_list+="${newer_list:+, }$latest"
    hints+="${hints:+,}{\"text\":$(json_str "Python $latest is available"),\"action\":\"python.update\"}"
  fi
done

while read -r name _version _rest; do
  case "$name" in "" | -*) continue ;; esac
  newer_list+="${newer_list:+, }$(tool_name "$name")"
  hints+="${hints:+,}{\"text\":$(json_str "A newer $(tool_name "$name") is available"),\"action\":\"python.update\"}"
done < <("$UV" tool list --outdated 2>/dev/null || true)

# a minor version newer than every installed one
newest=$(available_match "" || true)
if [ "${#versions[@]}" -gt 0 ] && [ -n "$newest" ] && newer "$(minor "$newest")" "$(minor "${versions[0]}")"; then
  hints+="${hints:+,}{\"text\":$(json_str "New minor version Python $newest"),\"action\":\"python.install\"}"
fi

if [ -n "$newer_list" ]; then
  printf '{"status":"warn","summary":%s,"hints":[%s]}\n' "$(json_str "Updates: $newer_list")" "$hints"
elif [ -n "$hints" ]; then
  printf '{"status":"ok","summary":"Up to date","hints":[%s]}\n' "$hints"
else
  printf '{"status":"ok","summary":"Up to date"}\n'
fi
