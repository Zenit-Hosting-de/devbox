#!/usr/bin/env bash
# Action: make an installed Node.js version the default.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
version=$(installed_match "$spec") || {
  sdk emit log error "Node.js $spec is not installed"
  exit 2
}
set_default "$version"
sdk emit result "Default Node.js: $version" "{\"version\":$(json_str "$version")}"
