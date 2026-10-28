#!/usr/bin/env bash
# Check: is a newer Maven version available (needs the network).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

current=$(current_version)
if [ -z "$current" ]; then
  printf '{"status":"ok","summary":"Maven is not installed"}\n'
  exit 0
fi
if ! latest=$(target_version "$(sdk setting version)" 2>/dev/null); then
  printf '{"status":"error","summary":%s}\n' "$(json_str "Version list not reachable: $(sdk setting metadataUrl)")"
  exit 0
fi
if [ "$latest" != "$current" ] && [ "$(printf '%s\n%s\n' "$current" "$latest" | sort -V | tail -n1)" = "$latest" ]; then
  printf '{"status":"warn","summary":%s,"hints":[{"text":%s,"action":"maven.update"}]}\n' "$(json_str "Update: $latest")" "$(json_str "$latest is available")"
else
  printf '{"status":"ok","summary":"Up to date"}\n'
fi
