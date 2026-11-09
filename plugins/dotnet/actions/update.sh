#!/usr/bin/env bash
# Action: install the newest SDK of every installed channel, take the workloads along
# and remove old SDK versions of these channels.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

keep=$(sdk setting keepPatches)
mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -eq 0 ]; then
  sdk emit result "No .NET SDK installed"
  exit 0
fi
previous=${versions[0]}

index=$(mktemp "$DEVBOX_CACHE_DIR/index.XXXXXX")
trap 'rm -f "$index"' EXIT
if ! fetch_index "$index"; then
  sdk emit log error "release metadata not reachable: $METADATA"
  exit 3
fi

declare -A done_channel=()
updated=""
planned=()
for v in "${versions[@]}"; do
  ch=$(channel_of "$v") || continue
  [ -n "${done_channel[$ch]:-}" ] && continue
  done_channel[$ch]=1
  latest=$(resolve_version "$ch" "$index") || {
    sdk emit log warn "no SDK found for .NET $ch"
    continue
  }
  # never go back to an older SDK than the installed one
  latest=$(newest "$v" "$latest")
  install_sdk "$latest"
  planned+=("$latest")
  [ "$latest" != "$v" ] && updated+="${updated:+, }$latest"
done

# in a dry run the new SDKs are only planned, so they count as installed here
mapfile -t versions < <(printf '%s\n' "${planned[@]}" "${versions[@]}" | sed '/^$/d' | sort -rVu)
install_workloads "${versions[0]}" "$previous"
set_env

# Keep the newest $keep SDKs per channel.
declare -A count=()
stay=()
drop=()
for v in "${versions[@]}"; do
  ch=$(channel_of "$v") || continue
  count[$ch]=$((${count[$ch]:-0} + 1))
  if [ "${count[$ch]}" -gt "$keep" ]; then
    drop+=("$v")
  else
    stay+=("$v")
  fi
done
for v in "${drop[@]}"; do
  remove_sdk "$v" "${stay[@]}"
done

sdk emit result "${updated:-Up to date}"
