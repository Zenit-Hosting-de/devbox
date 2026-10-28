#!/usr/bin/env bash
# Action: remove every installed Maven version and the shell environment block.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

removed=0
while IFS= read -r v; do
  [ -n "$v" ] || continue
  is_dry_run || rm -rf "${INSTALL_DIR:?}/$v"
  sdk emit change file delete "$INSTALL_DIR/$v" "Maven $v"
  removed=1
done < <(installed_versions)
if [ -L "$CURRENT" ]; then
  is_dry_run || rm -f "$CURRENT"
  sdk emit change link delete "$CURRENT" "no Maven"
fi
sdk block-remove "$ENV_FILE" maven
if [ "$removed" = 1 ]; then
  sdk emit result "Maven removed"
else
  sdk emit result "Maven is not installed"
fi
