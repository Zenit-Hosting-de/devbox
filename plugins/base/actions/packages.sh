#!/usr/bin/env bash
# Action: install the missing base and extra packages.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

pm=$(package_manager)
if [ -z "$pm" ]; then
  sdk emit log error "$no_manager_text"
  exit 3
fi
mapfile -t missing < <(missing_packages "$pm")
if [ "${#missing[@]}" -eq 0 ]; then
  sdk emit result "All base packages are installed"
  exit 0
fi
for p in "${missing[@]}"; do
  sdk emit change package create "$p" "install with $pm"
done
if is_dry_run; then
  sdk emit result "${#missing[@]} packages would be installed"
  exit 0
fi

sdk emit step install running "Install ${#missing[@]} packages"
case "$pm" in
apt)
  as_root apt-get update
  as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
  ;;
dnf) as_root dnf install -y "${missing[@]}" ;;
pacman) as_root pacman -S --needed --noconfirm "${missing[@]}" ;;
zypper) as_root zypper --non-interactive install "${missing[@]}" ;;
esac
sdk emit step install "done" "Install ${#missing[@]} packages"

mapfile -t still < <(missing_packages "$pm")
if [ "${#still[@]}" -gt 0 ]; then
  sdk emit log error "Still missing: ${still[*]}"
  exit 1
fi
sdk emit result "${#missing[@]} packages installed" "{\"installed\":$(json_list "${missing[@]}")}"
