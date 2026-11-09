#!/usr/bin/env bash
# Consumer: removes exactly the blocks of one registry credential, from
# gradle.properties and from the shell environment file.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
block="gradle:$name"
sdk emit step "remove-$name" running "$name"
drop_block "$PROPS" "$block"
drop_block "$ENV_FILE" "$block"
sdk emit step "remove-$name" "done" "$name"
sdk emit result "Gradle credential $name removed"
