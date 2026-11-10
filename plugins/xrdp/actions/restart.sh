#!/usr/bin/env bash
# Action: restart the services of xrdp, e.g. after a change of the configuration.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

mapfile -t units < <(services)
for unit in "${units[@]}"; do
  if ! is_dry_run && ! unit_exists "$unit"; then
    sdk emit log error "The service $unit does not exist. Install xrdp first (xrdp.install)."
    exit 3
  fi
done
for unit in "${units[@]}"; do
  sdk emit change service update "$unit" "restart"
done
if is_dry_run; then
  sdk emit result "xrdp would be restarted"
  exit 0
fi
sdk emit step restart running "Restart ${units[*]}"
as_root systemctl restart "${units[@]}"
sdk emit step restart "done" "Restart ${units[*]}"
sdk emit result "xrdp restarted" "{\"services\":$(json_list "${units[@]}")}"
