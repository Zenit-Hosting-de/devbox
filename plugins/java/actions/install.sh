#!/usr/bin/env bash
# Action: install the newest build of a JDK feature release.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

feature=$(sdk param version)
make_default=$(sdk param makeDefault)
is_dry_run || mkdir -p "$INSTALL_DIR"
version=$(install_feature "$feature")
if [ "$make_default" = true ]; then
  set_default "$version"
fi
sdk emit result "Temurin $version" "{\"version\":$(json_str "$version")}"
