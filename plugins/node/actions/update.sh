#!/usr/bin/env bash
# Action: update every installed major version to its newest patch release, take the
# global packages along and remove old patch versions.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

keep=$(sdk setting keepPatches)
default=$(default_version)
mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -eq 0 ]; then
  sdk emit result "No Node.js installed"
  exit 0
fi

index=$(mktemp "$DEVBOX_CACHE_DIR/index.XXXXXX")
trap 'rm -f "$index"' EXIT
if ! fetch_index "$index"; then
  sdk emit log error "distribution server not reachable: $DIST"
  exit 3
fi

declare -A done_major=()
updated=""
for v in "${versions[@]}"; do
  m=$(major "$v")
  [ -n "${done_major[$m]:-}" ] && continue
  done_major[$m]=1
  latest=$(resolve_version "$m" "$index") || {
    sdk emit log warn "no release found for Node.js $m"
    continue
  }
  # never go back to an older patch version than the installed one
  if [ "$(printf '%s\n%s\n' "$v" "$latest" | sort -V | tail -n1)" = "$v" ]; then
    latest=$v
  fi
  install_version "$latest"
  install_globals "$latest" "$v"
  if [ -n "$default" ] && [ "$(major "$default")" = "$m" ] && [ "$default" != "$latest" ]; then
    set_default "$latest"
    default=$latest
  fi
  [ "$latest" != "$v" ] && updated+="${updated:+, }$latest"
done

# Keep the newest $keep versions per major version, never the default.
declare -A count=()
while IFS= read -r v; do
  m=$(major "$v")
  count[$m]=$((${count[$m]:-0} + 1))
  if [ "${count[$m]}" -gt "$keep" ] && [ "$v" != "$default" ]; then
    is_dry_run || rm -rf "${INSTALL_DIR:?}/$v"
    sdk emit change file delete "$INSTALL_DIR/$v" "old patch version"
  fi
done < <(installed_versions)

sdk emit result "${updated:-Up to date}"
