#!/usr/bin/env bash
# Shared functions of the system plugin. Sourced by checks and actions.
# Uses only devbox sdk and the commands listed in requires.commands (plus coreutils).

sdk() { "$DEVBOX_BIN" sdk "$@"; }

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# setting_list KEY: the items of a string array setting, one per line.
setting_list() {
  { sdk setting "$1" | tr -d '[]"\n ' | tr ',' '\n'; echo; } | sed '/^$/d'
}

# host_field NAME: a field of the host section of the request (empty when missing).
host_field() { sdk json-get "$DEVBOX_REQUEST" "/host/$1" 2>/dev/null || true; }

# is_release_version VERSION: true for a released version (1.2.3, v1.2.3, 1.2.3-rc.1);
# false for development builds: 0.0.0-dev, a bare commit id, or the output of
# `git describe` between tags (1.2.3-4-gabcdef, 1.2.3-dirty, or as build metadata
# 1.2.3+4.gabcdef.dirty from make build).
is_release_version() {
  local v=${1#v}
  [ "$v" != 0.0.0-dev ] || return 1
  [[ $v =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || return 1
  [[ $v =~ -[0-9]+-g[0-9a-f]+ ]] && return 1
  [[ $v == *dirty* ]] && return 1
  return 0
}

# version_newer A B: true when version A is newer than version B (semver order: a
# prerelease is older than its release).
version_newer() {
  local a=${1#v} b=${2#v} ca cb pa pb
  a=${a%%+*}
  b=${b%%+*}
  ca=${a%%-*}
  cb=${b%%-*}
  pa=${a#"$ca"}
  pa=${pa#-}
  pb=${b#"$cb"}
  pb=${pb#-}
  if [ "$ca" != "$cb" ]; then
    [ "$(printf '%s\n%s\n' "$ca" "$cb" | sort -V | tail -n1)" = "$ca" ]
    return
  fi
  [ "$pa" != "$pb" ] || return 1
  [ -n "$pb" ] || return 1
  [ -n "$pa" ] || return 0
  [ "$(printf '%s\n%s\n' "$pa" "$pb" | sort -V | tail -n1)" = "$pa" ]
}

# archive_name VERSION ARCH: the release archive of a version, as GoReleaser names it.
archive_name() { echo "devbox_$1_linux_$2.tar.gz"; }

# fetch_releases FILE: the release list of releaseUrl.
fetch_releases() { sdk fetch "$(sdk setting releaseUrl)" >"$1"; }

# release_has_asset FILE INDEX NAME: whether release INDEX lists an asset NAME.
release_has_asset() {
  local names
  # capture first: grep -q would close the pipe early and make a pipefail pipeline fail with SIGPIPE
  names=$(sdk json-get "$1" "/$2/assets/*/name" 2>/dev/null || true)
  grep -xF -- "$3" <<<"$names" >/dev/null
}

# release_asset_url FILE INDEX NAME: the download address of an asset of a release.
release_asset_url() {
  local names urls i=0 n
  mapfile -t names < <(sdk json-get "$1" "/$2/assets/*/name" 2>/dev/null || true)
  mapfile -t urls < <(sdk json-get "$1" "/$2/assets/*/browser_download_url" 2>/dev/null || true)
  for n in "${names[@]}"; do
    if [ "$n" = "$3" ]; then
      echo "${urls[$i]:-}"
      return 0
    fi
    i=$((i + 1))
  done
  return 1
}

# newest_release FILE CHANNEL ARCH: sets LATEST_VERSION and LATEST_INDEX to the newest
# release of the channel (stable, prerelease) that has an archive for ARCH; tags are
# vX.Y.Z or devbox-vX.Y.Z. Drafts,
# tags that are not versions and releases without binaries are ignored. Returns 1
# when there is none.
# shellcheck disable=SC2034 # LATEST_VERSION and LATEST_INDEX are read by the callers
newest_release() {
  local file=$1 channel=$2 arch=$3 tags pres drafts i v
  LATEST_VERSION=""
  LATEST_INDEX=""
  mapfile -t tags < <(sdk json-get "$file" '/*/tag_name' 2>/dev/null || true)
  mapfile -t pres < <(sdk json-get "$file" '/*/prerelease' 2>/dev/null || true)
  mapfile -t drafts < <(sdk json-get "$file" '/*/draft' 2>/dev/null || true)
  for i in "${!tags[@]}"; do
    [ "${drafts[$i]:-false}" = true ] && continue
    [ "$channel" = stable ] && [ "${pres[$i]:-false}" = true ] && continue
    v=${tags[$i]#devbox-} # release-please component tags (devbox-vX.Y.Z)
    v=${v#v}
    [[ $v =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || continue
    if [ -n "$LATEST_VERSION" ] && ! version_newer "$v" "$LATEST_VERSION"; then
      continue
    fi
    release_has_asset "$file" "$i" "$(archive_name "$v" "$arch")" || continue
    LATEST_VERSION=$v
    LATEST_INDEX=$i
  done
  [ -n "$LATEST_VERSION" ]
}
