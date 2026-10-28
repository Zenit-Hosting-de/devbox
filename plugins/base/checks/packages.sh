#!/usr/bin/env bash
# Check: are the base and extra packages installed?
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

pm=$(package_manager)
if [ -z "$pm" ]; then
  printf '{"status":"error","summary":%s,"data":{"manager":null,"missing":[]}}\n' "$(json_str "$no_manager_text")"
  exit 0
fi
mapfile -t wanted < <(wanted_packages)
mapfile -t missing < <(missing_packages "$pm")
if [ "${#missing[@]}" -eq 0 ]; then
  printf '{"status":"ok","summary":%s,"data":{"manager":%s,"missing":[]},"details":[{"label":"Package manager","value":%s}]}\n' \
    "$(json_str "${#wanted[@]} base packages installed")" "$(json_str "$pm")" "$(json_str "$pm")"
else
  list=$(IFS=,; echo "${missing[*]}")
  printf '{"status":"missing","summary":%s,"data":{"manager":%s,"missing":%s},"hints":[{"text":"Install the base packages","action":"base.packages"}]}\n' \
    "$(json_str "Missing: ${list//,/, }")" "$(json_str "$pm")" "$(json_list "${missing[@]}")"
fi
