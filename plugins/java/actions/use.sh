#!/usr/bin/env bash
# Action: make an installed JDK the default.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

version=$(sdk param version)
if [ ! -x "$INSTALL_DIR/$version/bin/java" ]; then
  sdk emit log error "JDK $version is not installed"
  exit 2
fi
set_default "$version"
sdk emit result "Default JDK: $version" "{\"version\":$(json_str "$version")}"
