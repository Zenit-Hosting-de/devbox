#!/usr/bin/env bash
# Action: install a Maven version (default: the setting "version") and make it current.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

version=$(target_version "$(sdk param version)")
is_dry_run || mkdir -p "$INSTALL_DIR"
install_version "$version"
set_current "$version"
sdk emit result "Maven $version" "{\"version\":$(json_str "$version")}"
