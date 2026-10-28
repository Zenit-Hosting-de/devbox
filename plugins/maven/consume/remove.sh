#!/usr/bin/env bash
# Consumer: remove exactly the blocks that apply.sh wrote for this credential. The
# file stays as it was before, except for the empty sections devbox had created.
set -euo pipefail
# shellcheck source=../lib/consume.sh
. "$DEVBOX_PLUGIN_DIR/lib/consume.sh"

name=$(sdk instance)
check_name "$name"
for b in "maven:$name" "maven:$name:mirror" "maven:$name:profile" "maven:$name:active"; do
  sdk block-remove "$SETTINGS" "$b" --comment xml
done
drop_empty_sections
sdk emit result "Maven credential $name removed from $SETTINGS"
