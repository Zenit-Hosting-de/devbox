#!/usr/bin/env bash
# Consumer: removes exactly the blocks that apply.sh wrote for this credential, then
# the sections and the file DevBox added once nothing else is left in them.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
sdk emit step "remove-$name" running "$name"
drop_xml_block "nuget:$name"
drop_xml_block "nuget:$name:credentials"
drop_empty_sections
drop_env_block "nuget:$name"
sdk emit step "remove-$name" "done" "$name"
sdk emit result "NuGet credential $name removed"
