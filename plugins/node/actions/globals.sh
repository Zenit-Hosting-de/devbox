#!/usr/bin/env bash
# Action: install the configured global packages into the default Node.js version.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

version=$(default_version)
if [ -z "$version" ]; then
  sdk emit log warn "no default Node.js version; install one first"
  sdk emit result "No default Node.js installed"
  exit 0
fi
install_globals "$version"
sdk emit result "Global packages in Node.js $version" "{\"version\":$(json_str "$version")}"
