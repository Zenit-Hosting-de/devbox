#!/usr/bin/env bash
# Action: install the configured workloads into the newest .NET SDK, the one dotnet
# uses by default.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -eq 0 ]; then
  sdk emit log warn "no .NET SDK installed; install one first"
  sdk emit result "No .NET SDK installed"
  exit 0
fi
install_workloads "${versions[0]}"
sdk emit result "Workloads in .NET SDK ${versions[0]}" "{\"version\":$(json_str "${versions[0]}")}"
