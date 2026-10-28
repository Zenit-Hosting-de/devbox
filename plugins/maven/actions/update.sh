#!/usr/bin/env bash
# Action: install the wanted version (setting "version", newest 3.x for "latest"),
# make it current and delete the other versions.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if [ -z "$(installed_versions)" ]; then
  sdk emit result "Maven is not installed"
  exit 0
fi
before=$(current_version)
version=$(target_version "$(sdk setting version)")
install_version "$version"
set_current "$version"
while IFS= read -r v; do
  [ -n "$v" ] || continue
  [ "$v" = "$version" ] && continue
  is_dry_run || rm -rf "${INSTALL_DIR:?}/$v"
  sdk emit change file delete "$INSTALL_DIR/$v" "old version"
done < <(installed_versions)
if [ "$before" = "$version" ]; then
  sdk emit result "Up to date"
else
  sdk emit result "Maven $version" "{\"version\":$(json_str "$version")}"
fi
