#!/usr/bin/env bash
# Action: install the configured tools with uv tool install.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if ! has_uv; then
  if [ -n "$(setting_list tools)" ]; then
    sdk emit log warn "uv is not installed; install Python first"
  fi
  sdk emit result "uv is not installed"
  exit 0
fi
install_tools
sdk emit result "Tools installed"
