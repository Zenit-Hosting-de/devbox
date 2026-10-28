#!/usr/bin/env bash
# Check: installed Maven versions and the current one.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

current=$(current_version)
items=""
summary=""
while IFS= read -r v; do
  [ -n "$v" ] || continue
  is_current=false
  label=$v
  if [ "$v" = "$current" ]; then
    is_current=true
    label="$v (current)"
  fi
  items+="${items:+,}{\"version\":$(json_str "$v"),\"path\":$(json_str "$INSTALL_DIR/$v"),\"current\":$is_current}"
  summary+="${summary:+, }$label"
done < <(installed_versions)

if [ -z "$items" ]; then
  printf '{"status":"missing","summary":"Maven is not installed","data":{"items":[]},"hints":[{"text":"Install Maven","action":"maven.install"}]}\n'
elif [ -z "$current" ]; then
  printf '{"status":"warn","summary":%s,"data":{"items":[%s]},"hints":[{"text":"Install Maven again to set the current version","action":"maven.install"}]}\n' "$(json_str "Maven $summary")" "$items"
else
  printf '{"status":"ok","summary":%s,"data":{"items":[%s]},"details":[{"label":"MAVEN_HOME","value":%s}]}\n' "$(json_str "Maven $summary")" "$items" "$(json_str "$CURRENT")"
fi
