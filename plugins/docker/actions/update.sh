#!/usr/bin/env bash
# Action: update the installed Docker packages through the package manager. A change
# is reported only for packages whose version differs afterwards. Without installed
# packages there is nothing to do, so the group update-all runs on every machine.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

pm=$(package_manager)
[ -n "$pm" ] || fail "$no_manager_text" 3
mapfile -t wanted < <(wanted_packages "$pm")
installed=()
before=()
for p in "${wanted[@]}"; do
  if v=$(installed_version "$pm" "$p"); then
    installed+=("$p")
    before+=("$v")
  fi
done
if [ "${#installed[@]}" -eq 0 ]; then
  sdk emit result "Docker Engine is not installed with $pm from the source $(sdk setting source)"
  exit 0
fi

if is_dry_run; then
  sdk emit change package update "${installed[*]}" "update with $pm when a newer version is available"
  sdk emit result "Docker Engine would be updated with $pm"
  exit 0
fi

sdk emit step update running "Update ${#installed[@]} packages"
case "$pm" in
apt)
  as_root apt-get update
  as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y --only-upgrade "${installed[@]}"
  ;;
dnf) as_root dnf upgrade -y "${installed[@]}" ;;
pacman) as_root pacman -S --needed --noconfirm "${installed[@]}" ;;
zypper) as_root zypper --non-interactive update "${installed[@]}" ;;
esac
sdk emit step update "done" "Update ${#installed[@]} packages"

updated=0
for i in "${!installed[@]}"; do
  after=$(installed_version "$pm" "${installed[$i]}") || after=""
  if [ "$after" != "${before[$i]}" ]; then
    updated=$((updated + 1))
    sdk emit change package update "${installed[$i]}" "from ${before[$i]} to $after"
  fi
done
if [ "$updated" -eq 0 ]; then
  sdk emit result "Docker Engine is up to date"
else
  sdk emit result "$updated packages updated with $pm"
fi
