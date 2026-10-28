#!/usr/bin/env bash
# Check: installed JDKs and the default one.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

default=$(default_version)
items=""
summary=""
while IFS= read -r v; do
  [ -n "$v" ] || continue
  is_default=false
  label=$v
  if [ "$v" = "$default" ]; then
    is_default=true
    label="$v (default)"
  fi
  items+="${items:+,}{\"version\":$(json_str "$v"),\"path\":$(json_str "$INSTALL_DIR/$v"),\"default\":$is_default}"
  summary+="${summary:+, }$label"
done < <(installed_versions)

if [ -z "$items" ]; then
  printf '{"status":"missing","summary":"No JDK installed","data":{"items":[]},"hints":[{"text":"Install a JDK","action":"java.install"}]}\n'
elif [ -z "$default" ]; then
  printf '{"status":"warn","summary":%s,"data":{"items":[%s]},"hints":[{"text":"Choose a default JDK","action":"java.use"}]}\n' "$(json_str "Temurin $summary")" "$items"
else
  printf '{"status":"ok","summary":%s,"data":{"items":[%s]},"details":[{"label":"JAVA_HOME","value":%s}]}\n' "$(json_str "Temurin $summary")" "$items" "$(json_str "$CURRENT")"
fi
