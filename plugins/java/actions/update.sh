#!/usr/bin/env bash
# Action: update every installed feature release and remove old patch versions.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

keep=$(sdk setting keepPatches)
default=$(default_version)
mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -eq 0 ]; then
  sdk emit result "No JDK installed"
  exit 0
fi

declare -A done_feature=()
updated=""
for v in "${versions[@]}"; do
  f=$(major "$v")
  [ -n "${done_feature[$f]:-}" ] && continue
  done_feature[$f]=1
  latest=$(install_feature "$f")
  if [ -n "$default" ] && [ "$(major "$default")" = "$f" ] && [ "$default" != "$latest" ]; then
    set_default "$latest"
    default=$latest
  fi
  [ "$latest" != "$v" ] && updated+="${updated:+, }$latest"
done

# Keep the newest $keep versions per feature release, never the default.
declare -A count=()
while IFS= read -r v; do
  f=$(major "$v")
  count[$f]=$((${count[$f]:-0} + 1))
  if [ "${count[$f]}" -gt "$keep" ] && [ "$v" != "$default" ]; then
    is_dry_run || rm -rf "${INSTALL_DIR:?}/$v"
    sdk emit change file delete "$INSTALL_DIR/$v" "old patch version"
  fi
done < <(installed_versions)

sdk emit result "${updated:-Up to date}"
