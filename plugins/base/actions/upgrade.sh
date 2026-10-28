#!/usr/bin/env bash
# Action: update all packages of the system.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

pm=$(package_manager)
if [ -z "$pm" ]; then
  sdk emit log error "$no_manager_text"
  exit 3
fi
mode=$(sdk setting upgradeMode)
if is_dry_run; then
  sdk emit change package update system "update all packages with $pm ($mode)"
  sdk emit result "The system would be updated with $pm"
  exit 0
fi

sdk emit step upgrade running "Update the system"
case "$pm:$mode" in
apt:safe) as_root apt-get update && as_root env DEBIAN_FRONTEND=noninteractive apt-get upgrade -y ;;
apt:full) as_root apt-get update && as_root env DEBIAN_FRONTEND=noninteractive apt-get full-upgrade -y ;;
dnf:*) as_root dnf upgrade -y ;;
pacman:*) as_root pacman -Syu --noconfirm ;;
zypper:safe) as_root zypper --non-interactive update ;;
zypper:full) as_root zypper --non-interactive dist-upgrade ;;
esac
sdk emit step upgrade "done" "Update the system"
sdk emit change package update system "updated with $pm ($mode)"
sdk emit result "System updated with $pm"
