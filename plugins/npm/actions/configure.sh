#!/usr/bin/env bash
# Action: sets the default registry in a marked block of the npm configuration file.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

registry=$(sdk param registry)
if [ -z "$registry" ]; then
  sdk emit log error "no registry given"
  exit 2
fi
registry=$(slash "$registry")

sdk emit step configure running "$registry"
printf 'registry=%s\n' "$registry" | sdk block "$NPMRC" npm
sdk emit step configure "done" "$registry"
