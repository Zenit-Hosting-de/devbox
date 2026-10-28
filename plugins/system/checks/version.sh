#!/usr/bin/env bash
# Check: installed DevBox version against the newest release of the channel (needs the
# network; the result is cached). A development build is not compared.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

current=${DEVBOX_VERSION:-}
if ! is_release_version "$current"; then
  printf '{"status":"ok","summary":%s,"data":{"version":%s,"development":true}}\n' \
    "$(json_str "Development build ${current:-unknown}, not compared with releases")" "$(json_str "$current")"
  exit 0
fi
current=${current#v}

url=$(sdk setting releaseUrl)
channel=$(sdk setting updateChannel)
arch=$(host_field arch)
file=$(mktemp "$DEVBOX_CACHE_DIR/releases.XXXXXX")
trap 'rm -f "$file"' EXIT

if ! fetch_releases "$file" 2>/dev/null; then
  printf '{"status":"error","summary":%s}\n' "$(json_str "Release list not reachable: $url")"
  exit 0
fi
if ! newest_release "$file" "$channel" "$arch"; then
  printf '{"status":"ok","summary":%s,"data":{"version":%s}}\n' \
    "$(json_str "DevBox $current, no release with binaries found ($channel)")" "$(json_str "$current")"
  exit 0
fi

if version_newer "$LATEST_VERSION" "$current"; then
  printf '{"status":"warn","summary":%s,"data":{"version":%s,"latest":%s},"hints":[{"text":%s,"action":"system.update"}]}\n' \
    "$(json_str "DevBox $current, $LATEST_VERSION is available")" "$(json_str "$current")" "$(json_str "$LATEST_VERSION")" \
    "$(json_str "Update DevBox to $LATEST_VERSION")"
else
  printf '{"status":"ok","summary":%s,"data":{"version":%s,"latest":%s}}\n' \
    "$(json_str "DevBox $current is up to date")" "$(json_str "$current")" "$(json_str "$LATEST_VERSION")"
fi
