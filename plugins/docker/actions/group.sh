#!/usr/bin/env bash
# Action: add the user to the Docker group, creating the group when it is missing.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

user=$(group_user)
group=$(sdk setting group)
[ -n "$group" ] || fail "The setting group is empty" 2

if in_group "$user" "$group"; then
  sdk emit result "$user is a member of the group $group"
  exit 0
fi
for cmd in getent groupadd usermod; do
  command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is not in PATH" 3
done

create=false
if ! getent group "$group" >/dev/null 2>&1; then
  create=true
  sdk emit change other create "group $group" "create the group"
fi
sdk emit change other update "group $group" "add $user"
if is_dry_run; then
  sdk emit result "$user would be added to the group $group"
  exit 0
fi

! $create || as_root groupadd --system "$group"
as_root usermod -aG "$group" "$user"
in_group "$user" "$group" || fail "$user is still not a member of the group $group"
sdk emit notice relogin "The group $group takes effect after the next login"
sdk emit result "$user added to the group $group"
