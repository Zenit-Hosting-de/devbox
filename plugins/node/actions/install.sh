#!/usr/bin/env bash
# Action: install a Node.js release (the newest of a major version, lts or current).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
make_default=$(sdk param makeDefault)
if [ -n "$spec" ] && ! valid_spec "${spec#v}"; then
  sdk emit log error "invalid version: $spec"
  exit 2
fi

index=$(mktemp "$DEVBOX_CACHE_DIR/index.XXXXXX")
trap 'rm -f "$index"' EXIT
if ! fetch_index "$index"; then
  sdk emit log error "distribution server not reachable: $DIST"
  exit 3
fi
version=$(resolve_version "$spec" "$index") || {
  sdk emit log error "no Node.js release matches '${spec:-$CHANNEL}'"
  exit 2
}

install_version "$version"
install_globals "$version"
if [ "$make_default" = true ]; then
  set_default "$version"
fi
sdk emit result "Node.js $version" "{\"version\":$(json_str "$version")}"
