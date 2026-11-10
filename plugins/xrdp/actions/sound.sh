#!/usr/bin/env bash
# Action: install the xrdp module of the sound server when the setting sound is on.
# With sound switched off nothing is installed and nothing is removed.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if [ "$(sdk setting sound)" != true ]; then
  sdk emit result "Sound is switched off" "{\"sound\":false,\"installed\":[]}"
  exit 0
fi
pm=$(package_manager)
if [ -z "$pm" ]; then
  sdk emit log error "$no_manager_text"
  exit 3
fi
mapfile -t wanted < <(manager_items soundPackages "$pm" | awk 'NF && !seen[$0]++')
if [ "${#wanted[@]}" -eq 0 ]; then
  sdk emit log error "No sound packages are set for $pm (setting xrdp.soundPackages)"
  exit 3
fi
mapfile -t missing < <(missing_of "$pm" "${wanted[@]}")
if [ "${#missing[@]}" -eq 0 ]; then
  sdk emit result "The sound module is installed" "{\"sound\":true,\"installed\":[]}"
  exit 0
fi
for p in "${missing[@]}"; do
  sdk emit change package create "$p" "install with $pm"
done
if is_dry_run; then
  sdk emit result "${#missing[@]} packages would be installed"
  exit 0
fi

sdk emit step sound running "Install the sound module"
install_packages "$pm" "${missing[@]}"
sdk emit step sound "done" "Install the sound module"

mapfile -t still < <(missing_of "$pm" "${wanted[@]}")
if [ "${#still[@]}" -gt 0 ]; then
  sdk emit log error "Still missing: ${still[*]}"
  exit 1
fi
sdk emit notice relogin "Remote sessions started from now on play sound on the client."
sdk emit result "${#missing[@]} packages installed" "{\"sound\":true,\"installed\":$(json_list "${missing[@]}")}"
