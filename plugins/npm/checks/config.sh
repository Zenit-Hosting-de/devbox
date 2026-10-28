#!/usr/bin/env bash
# Check: effective registry and scopes of the npm configuration file.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

default=$(sdk setting defaultRegistry)
registry=""
scopes=""
summary_scopes=""
if [ -f "$NPMRC" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    line=${line%$'\r'}
    case "$line" in
    '' | '#'* | ';'*) continue ;;
    esac
    key=${line%%=*}
    value=${line#*=}
    key=${key//[[:space:]]/}
    value=${value#"${value%%[![:space:]]*}"}
    value=${value%"${value##*[![:space:]]}"}
    case "$key" in
    registry) registry=$value ;;
    @*:registry)
      scope=${key%:registry}
      scopes+="${scopes:+,}{\"scope\":$(json_str "$scope"),\"registry\":$(json_str "$value")}"
      summary_scopes+="${summary_scopes:+, }$scope"
      ;;
    esac
  done <"$NPMRC"
fi

if [ -z "$registry" ]; then
  printf '{"status":"missing","summary":%s,"data":{"registry":%s,"configured":false,"scopes":[%s]},"hints":[{"text":"Set the default registry","action":"npm.configure"}]}\n' \
    "$(json_str "$default (default, not set in $NPMRC)")" "$(json_str "$default")" "$scopes"
else
  summary=$registry
  [ -z "$summary_scopes" ] || summary+=" ($summary_scopes)"
  printf '{"status":"ok","summary":%s,"data":{"registry":%s,"configured":true,"mode":%s,"scopes":[%s]},"details":[{"label":"File","value":%s}]}\n' \
    "$(json_str "$summary")" "$(json_str "$registry")" "$(json_str "$MODE")" "$scopes" "$(json_str "$NPMRC")"
fi
