#!/usr/bin/env bash
# Action: stop the services of xrdp and do not start them with the machine any more.
# Packages and configuration stay.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

mapfile -t units < <(services)
todo=()
for unit in "${units[@]}"; do
  unit_exists "$unit" || continue
  if unit_enabled "$unit" || unit_active "$unit"; then
    sdk emit change service update "$unit" "stop and disable"
    todo+=("$unit")
  fi
done
if [ "${#todo[@]}" -eq 0 ]; then
  sdk emit result "xrdp is switched off"
  exit 0
fi
if is_dry_run; then
  sdk emit result "xrdp would be switched off"
  exit 0
fi
sdk emit step disable running "Stop and disable ${todo[*]}"
as_root systemctl disable --now "${todo[@]}"
sdk emit step disable "done" "Stop and disable ${todo[*]}"
sdk emit result "xrdp is switched off" "{\"services\":$(json_list "${todo[@]}")}"
