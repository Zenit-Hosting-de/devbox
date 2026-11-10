#!/usr/bin/env bash
# Action: install a .NET SDK (the newest of a channel, lts, sts, latest or an exact
# version) and the configured workloads.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
if [ -n "$spec" ] && ! valid_spec "$spec"; then
  sdk emit log error "invalid version: $spec"
  exit 2
fi

index=$(mktemp "$DEVBOX_CACHE_DIR/index.XXXXXX")
trap 'rm -f "$index"' EXIT
if ! fetch_index "$index"; then
  sdk emit log error "release metadata not reachable: $METADATA"
  exit 3
fi
version=$(resolve_version "$spec" "$index") || {
  sdk emit log error "no .NET SDK matches '${spec:-$CHANNEL}'"
  exit 2
}

install_sdk "$version"
mapfile -t versions < <(installed_versions)
install_workloads "$(newest "$version" "${versions[@]}")"
set_env
sdk emit result ".NET SDK $version" "{\"version\":$(json_str "$version")}"
