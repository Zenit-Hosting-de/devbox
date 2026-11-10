#!/usr/bin/env bash
# Action: remove an installed Python version.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
if ! valid_spec "$spec"; then
  sdk emit log error "invalid version: $spec"
  exit 2
fi
version=$(installed_match "$spec") || {
  sdk emit result "Python $spec is not installed"
  exit 0
}
path=""
while read -r v p; do
  if [ "$v" = "$version" ]; then path=$p; fi
done < <(installed_list)
if [ "$(default_version)" = "$version" ]; then
  sdk emit change link delete "$BIN_DIR/python" "no default Python"
fi
if ! is_dry_run; then
  if ! run_uv python uninstall "$version"; then
    sdk emit log error "uv python uninstall $version failed"
    exit 1
  fi
fi
sdk emit change file delete "${path:-$PYTHON_DIR}" "Python $version"
sdk emit result "Removed Python $version"
