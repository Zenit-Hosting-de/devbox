#!/usr/bin/env bash
# Check: installed .NET SDKs. dotnet uses the newest one unless a global.json of the
# project asks for another.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

items=""
summary=""
first=true
while IFS= read -r v; do
  [ -n "$v" ] || continue
  label=$v
  if $first; then
    label="$v (default)"
    is_default=true
    first=false
  else
    is_default=false
  fi
  items+="${items:+,}{\"version\":$(json_str "$v"),\"channel\":$(json_str "$(channel_of "$v" || true)"),\"path\":$(json_str "$INSTALL_DIR/sdk/$v"),\"default\":$is_default}"
  summary+="${summary:+, }$label"
done < <(installed_versions)

if [ -z "$items" ]; then
  printf '{"status":"missing","summary":"No .NET SDK installed","data":{"items":[]},"hints":[{"text":"Install a .NET SDK","action":"dotnet.install"}]}\n'
else
  printf '{"status":"ok","summary":%s,"data":{"items":[%s]},"details":[{"label":"DOTNET_ROOT","value":%s}]}\n' "$(json_str ".NET SDK $summary")" "$items" "$(json_str "$INSTALL_DIR")"
fi
