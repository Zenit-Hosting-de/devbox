#!/usr/bin/env bash
# Action: remove an installed JDK.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

version=$(sdk param version)
dir="$INSTALL_DIR/$version"
if [ ! -d "$dir" ]; then
  sdk emit result "JDK $version is not installed"
  exit 0
fi
if [ "$(default_version)" = "$version" ]; then
  is_dry_run || rm -f "$CURRENT"
  sdk emit change link delete "$CURRENT" "no default JDK"
  sdk block-remove "$ENV_FILE" java
fi
is_dry_run || rm -rf "${dir:?}"
sdk emit change file delete "$dir" "JDK $version"
sdk emit result "Removed JDK $version"
