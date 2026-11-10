#!/usr/bin/env bash
# Action: install xrdp and, with the desktop xfce, XFCE from the package sources of the
# distribution. Only the missing packages are installed.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

pm=$(package_manager)
if [ -z "$pm" ]; then
  sdk emit log error "$no_manager_text"
  exit 3
fi
mapfile -t server < <(manager_items packages "$pm")
if [ "${#server[@]}" -eq 0 ]; then
  sdk emit log error "No xrdp packages are set for $pm (setting xrdp.packages)"
  exit 3
fi
mapfile -t wanted < <(server_packages "$pm")
mapfile -t missing < <(missing_of "$pm" "${wanted[@]}")
if [ "${#missing[@]}" -eq 0 ]; then
  sdk emit result "xrdp is installed" "{\"installed\":[]}"
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
install_packages "$pm" "${missing[@]}"
sdk emit step install "done" "Install ${#missing[@]} packages"

mapfile -t still < <(missing_of "$pm" "${wanted[@]}")
if [ "${#still[@]}" -gt 0 ]; then
  sdk emit log error "Still missing: ${still[*]}"
  exit 1
fi
sdk emit result "${#missing[@]} packages installed" "{\"installed\":$(json_list "${missing[@]}")}"
