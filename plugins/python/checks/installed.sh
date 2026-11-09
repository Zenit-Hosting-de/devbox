#!/usr/bin/env bash
# Check: uv, the installed Python versions, the default one and the configured tools.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"
export UV_OFFLINE=1

if ! has_uv; then
  printf '{"status":"missing","summary":"uv is not installed","data":{"items":[],"tools":[]},"hints":[{"text":"Install Python","action":"python.install"}]}\n'
  exit 0
fi
uv=$(uv_version)
default=$(default_version)

items=""
summary=""
while read -r v p; do
  [ -n "$v" ] || continue
  is_default=false
  label=$v
  if [ "$v" = "$default" ]; then
    is_default=true
    label="$v (default)"
  fi
  items+="${items:+,}{\"version\":$(json_str "$v"),\"path\":$(json_str "$p"),\"default\":$is_default}"
  summary+="${summary:+, }$label"
done < <(installed_list)

tools=""
tool_names=""
declare -A have=()
while read -r name version; do
  [ -n "$name" ] || continue
  have[$name]=1
  tools+="${tools:+,}{\"name\":$(json_str "$name"),\"version\":$(json_str "$version")}"
  tool_names+="${tool_names:+, }$name $version"
done < <(tool_lines)

hints=""
missing=""
while IFS= read -r spec; do
  [ -n "$spec" ] || continue
  installed_match "$spec" >/dev/null || missing+="${missing:+, }$spec"
done < <(setting_list versions)
[ -n "$missing" ] && hints+="${hints:+,}{\"text\":$(json_str "Python $missing is not installed"),\"action\":\"python.install\"}"
missing_tools=""
while IFS= read -r spec; do
  [ -n "$spec" ] || continue
  name=$(tool_name "$spec")
  [ -n "${have[$name]:-}" ] || missing_tools+="${missing_tools:+, }$name"
done < <(setting_list tools)
[ -n "$missing_tools" ] && hints+="${hints:+,}{\"text\":$(json_str "Tools not installed: $missing_tools"),\"action\":\"python.tools\"}"

details="{\"label\":\"uv\",\"value\":$(json_str "$uv")},{\"label\":\"PATH\",\"value\":$(json_str "$BIN_DIR")}"
[ -n "$tool_names" ] && details+=",{\"label\":\"Tools\",\"value\":$(json_str "$tool_names")}"
data="{\"uv\":$(json_str "$uv"),\"items\":[$items],\"tools\":[$tools]}"

if [ -z "$items" ]; then
  printf '{"status":"missing","summary":"No Python installed","data":%s,"details":[%s],"hints":[{"text":"Install Python","action":"python.install"}]}\n' "$data" "$details"
  exit 0
fi
status=ok
if [ -z "$default" ]; then
  status=warn
  hints+="${hints:+,}{\"text\":\"Choose a default Python version\",\"action\":\"python.use\"}"
fi
[ -n "$hints" ] && status=warn
printf '{"status":"%s","summary":%s,"data":%s,"details":[%s],"hints":[%s]}\n' \
  "$status" "$(json_str "Python $summary")" "$data" "$details" "$hints"
