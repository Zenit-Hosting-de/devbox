#!/usr/bin/env bash
# Consumer: removes exactly the blocks of one registry credential, in both modes and
# for all tools, so a change of the settings in between leaves nothing behind.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
block="pip:$name"
sdk emit step "remove-$name" running "$name"
drop_block "$PIP_CONF" "$block"
drop_block "$UV_CONF" "$block"
drop_block "$NETRC" "$block"
drop_block "$ENV_FILE" "$block"
sdk emit step "remove-$name" "done" "$name"
sdk emit result "Index of $name removed"
