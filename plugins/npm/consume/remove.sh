#!/usr/bin/env bash
# Consumer: removes exactly the blocks of one registry credential.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
block="npm:$name"
sdk emit step "remove-$name" running "$name"
drop_block "$NPMRC" "$block"
drop_block "$ENV_FILE" "$block"
sdk emit step "remove-$name" "done" "$name"
