#!/usr/bin/env bash
# Action: make an installed Python version the default (python and python3 in PATH).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
if ! valid_spec "$spec"; then
  sdk emit log error "invalid version: $spec"
  exit 2
fi
version=$(installed_match "$spec") || {
  sdk emit log error "Python $spec is not installed"
  exit 2
}
set_default "$version"
sdk emit result "Default Python: $version" "{\"version\":$(json_str "$version")}"
