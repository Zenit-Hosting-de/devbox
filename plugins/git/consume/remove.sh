#!/usr/bin/env bash
# Consumer: takes back what apply.sh wrote for one git-host instance: the token at the
# helper and the marked block in the Git configuration. Address, user name and helper
# come from the block, so a credential whose fields changed since is removed as well.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
[[ $name =~ ^[a-z][a-z0-9-]{1,31}$ ]] || fail "invalid instance name $name"
block="git:credential:$name"

origin=""
user=""
spec=""
if has_block "$GITCONFIG" "$block"; then
  origin=$(block_origin "$GITCONFIG" "$block")
  user=$(block_value "$GITCONFIG" "$block" username)
  spec=$(block_value "$GITCONFIG" "$block" helper)
else
  # nothing in the configuration: still take a token that an earlier run left behind
  url=$(sdk field url 2>/dev/null) || url=""
  user=$(sdk field username 2>/dev/null) || user=""
  origin=$(origin_of "$url" 2>/dev/null) || origin=""
  spec=$(helper_spec)
fi

sdk emit step "remove-$name" running "${origin:-$name}"
if [ -n "$origin" ] && [ -n "$user" ] && [ -n "$spec" ]; then
  forget "$spec" "${origin#https://}" "$user"
fi
drop_block "$GITCONFIG" "$block"
sdk emit step "remove-$name" "done" "${origin:-$name}"
sdk emit result "Git credential $name removed"
