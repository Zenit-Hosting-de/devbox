#!/usr/bin/env bash
# Action: remove an installed Node.js version.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
version=$(installed_match "$spec") || {
  sdk emit result "Node.js $spec is not installed"
  exit 0
}
dir="$INSTALL_DIR/$version"
if [ "$(default_version)" = "$version" ]; then
  is_dry_run || rm -f "$CURRENT"
  sdk emit change link delete "$CURRENT" "no default Node.js"
  sdk block-remove "$ENV_FILE" node
fi
is_dry_run || rm -rf "${dir:?}"
sdk emit change file delete "$dir" "Node.js $version"
sdk emit result "Removed Node.js $version"
