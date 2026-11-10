#!/usr/bin/env bash
# Action: start the services of xrdp now and at every start of the machine.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

mapfile -t units < <(services)
todo=()
for unit in "${units[@]}"; do
  if ! unit_exists "$unit"; then
    if is_dry_run; then
      # in the dry run of a setup plan the packages are not installed yet
      sdk emit change service update "$unit" "enable and start"
      todo+=("$unit")
      continue
    fi
    sdk emit log error "The service $unit does not exist. Install xrdp first (xrdp.install)."
    exit 3
  fi
  if unit_enabled "$unit" && unit_active "$unit"; then
    continue
  fi
  sdk emit change service update "$unit" "enable and start"
  todo+=("$unit")
done
if [ "${#todo[@]}" -eq 0 ]; then
  sdk emit result "xrdp is switched on"
  exit 0
fi
if is_dry_run; then
  sdk emit result "xrdp would be switched on"
  exit 0
fi
sdk emit step enable running "Enable and start ${todo[*]}"
as_root systemctl enable --now "${todo[@]}"
sdk emit step enable "done" "Enable and start ${todo[*]}"
sdk emit result "xrdp is switched on" "{\"services\":$(json_list "${todo[@]}")}"
