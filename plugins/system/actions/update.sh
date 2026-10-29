#!/usr/bin/env bash
# Action: replace the devbox binary by the newest release of the update channel. The
# download is verified against checksums.txt of the release; the binary is replaced
# atomically and the old one stays as <binary>.previous.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

current=${DEVBOX_VERSION:-}
force=$(sdk param force)

if ! is_release_version "$current" && [ "$force" != true ]; then
  sdk emit result "Development build ${current:-unknown} is not replaced"
  exit 0
fi

url=$(sdk setting releaseUrl)
channel=$(sdk setting updateChannel)
arch=$(host_field arch)
[ -n "$arch" ] || {
  sdk emit log error "The architecture of this machine is unknown"
  exit 3
}

work=$(mktemp -d "$DEVBOX_CACHE_DIR/update.XXXXXX")
trap 'rm -rf "$work"' EXIT

sdk emit step releases running "Look for new releases"
if ! fetch_releases "$work/releases.json" 2>/dev/null; then
  sdk emit step releases failed "Look for new releases"
  sdk emit log error "Release list not reachable: $url"
  exit 3
fi
if ! newest_release "$work/releases.json" "$channel" "$arch"; then
  sdk emit step releases "done" "Look for new releases"
  sdk emit result "No release with binaries found ($channel)"
  exit 0
fi
sdk emit step releases "done" "Look for new releases"

latest=$LATEST_VERSION
if is_release_version "$current" && ! version_newer "$latest" "${current#v}"; then
  sdk emit result "DevBox ${current#v} is up to date"
  exit 0
fi

bin=$(readlink -f "$DEVBOX_BIN")
bin_dir=$(dirname "$bin")
if [ ! -w "$bin_dir" ]; then
  sdk emit log error "$bin_dir is not writable. DevBox is probably installed by a package manager; update it there, or install it into a folder of the user."
  exit 3
fi

archive=$(archive_name "$latest" "$arch")
archive_url=$(release_asset_url "$work/releases.json" "$LATEST_INDEX" "$archive")
sums_url=$(release_asset_url "$work/releases.json" "$LATEST_INDEX" checksums.txt || true)
if [ -z "$sums_url" ]; then
  sdk emit log error "Release $latest has no checksums.txt; refusing to install an unverified binary."
  exit 1
fi

if is_dry_run; then
  sdk emit change file update "$bin" "replace ${current:-unknown} with $latest"
  sdk emit change file create "$bin.previous" "keep the current binary"
  sdk emit result "DevBox would be updated to $latest"
  exit 0
fi

sdk emit step download running "Download DevBox $latest"
sdk download "$sums_url" "$work/checksums.txt"
sum=$(awk -v n="$archive" '{ f = $2; sub(/^\*/, "", f) } f == n { print $1; exit }' "$work/checksums.txt")
if [ -z "$sum" ]; then
  sdk emit step download failed "Download DevBox $latest"
  sdk emit log error "checksums.txt of release $latest does not list $archive."
  exit 1
fi
if ! sdk download "$archive_url" "$work/$archive" --sha256 "$sum"; then
  sdk emit step download failed "Download DevBox $latest"
  sdk emit log error "The download of $archive does not match its checksum; the binary was not replaced."
  exit 1
fi
sdk emit step download "done" "Download DevBox $latest"

sdk emit step replace running "Replace the binary"
mkdir "$work/unpacked"
tar -xzf "$work/$archive" -C "$work/unpacked" devbox
if [ ! -f "$work/unpacked/devbox" ]; then
  sdk emit step replace failed "Replace the binary"
  sdk emit log error "$archive contains no devbox binary."
  exit 1
fi
# The temporary file lies next to the binary so that mv is an atomic rename.
new=$(mktemp "$bin_dir/.devbox.XXXXXX")
trap 'rm -rf "$work" "$new"' EXIT
cat "$work/unpacked/devbox" >"$new"
chmod 0755 "$new"
# The old inode stays reachable as .previous and under its own name in the work folder
# (hard link, copy as fallback).
mkdir "$work/self"
self=$work/self/devbox
ln "$bin" "$self" 2>/dev/null || cp -p "$bin" "$self"
previous_op=create
[ ! -e "$bin.previous" ] || previous_op=update
ln -f "$bin" "$bin.previous" 2>/dev/null || cp -p "$bin" "$bin.previous"
mv -f "$new" "$bin"
# From here on sdk calls go through the old binary (the one that runs this action), not
# through the new one that now sits at its path.
DEVBOX_BIN=$self
sdk emit change file update "$bin" "replaced ${current:-unknown} with $latest"
sdk emit change file "$previous_op" "$bin.previous" "previous binary"
sdk emit step replace "done" "Replace the binary"
# A running `devbox serve` still has the old binary in memory; this makes it restart
# with the new one. Not fatal: the binary is replaced, and a core that does not know
# the notice yet (a plugin newer than the binary) only skips the restart.
sdk emit notice restart-service "A running DevBox service restarts with the new version." || true
sdk emit result "DevBox updated to $latest" "{\"version\":\"$latest\"}"
