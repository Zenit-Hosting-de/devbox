#!/usr/bin/env bash
# Shared functions of the maven plugin. Sourced by checks, actions and consumers.
# Uses only devbox sdk and the commands listed in requires.commands.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

INSTALL_DIR=$(sdk setting installDir)
ENV_FILE=$(sdk setting envFile)
CURRENT="$INSTALL_DIR/current"

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# installed_versions: installed Maven versions, newest first.
installed_versions() {
  local d
  [ -d "$INSTALL_DIR" ] || return 0
  for d in "$INSTALL_DIR"/*/; do
    d=${d%/}
    [ -L "$d" ] && continue
    [ -x "$d/bin/mvn" ] && basename "$d"
  done | sort -rV
}

# current_version: the version "current" points to, if any.
current_version() {
  local target
  target=$(readlink "$CURRENT" 2>/dev/null) || return 0
  [ -x "$target/bin/mvn" ] && basename "$target"
  return 0
}

# latest_version: the newest 3.x release in the version list (final releases only).
latest_version() {
  local meta line found
  meta=$(mktemp "$DEVBOX_CACHE_DIR/metadata.XXXXXX")
  if ! sdk fetch "$(sdk setting metadataUrl)" >"$meta"; then
    rm -f "$meta"
    return 1
  fi
  found=$(
    while IFS= read -r line; do
      if [[ $line =~ \<version\>(3\.[0-9]+\.[0-9]+)\</version\> ]]; then
        echo "${BASH_REMATCH[1]}"
      fi
    done <"$meta" | sort -V | tail -n1
  )
  rm -f "$meta"
  [ -n "$found" ] || return 1
  echo "$found"
}

# target_version WANTED: WANTED, or the newest 3.x release for "latest".
target_version() {
  if [ "$1" = latest ]; then
    latest_version || {
      sdk emit log error "version list not reachable or empty: $(sdk setting metadataUrl)"
      return 3
    }
  else
    echo "$1"
  fi
}

# install_version VERSION: installs VERSION unless it is there already, after
# verifying the SHA-512 checksum of the archive.
install_version() {
  local version=$1 dir="$INSTALL_DIR/$1" base archive sumfile expected got tmp
  if [ -x "$dir/bin/mvn" ]; then
    sdk emit step "install-$version" skipped "Maven $version"
    return 0
  fi
  sdk emit step "install-$version" running "Maven $version"
  if is_dry_run; then
    sdk emit change file create "$dir" "Maven $version"
  else
    base="$(sdk setting distUrl)/$version/binaries/apache-maven-$version-bin.tar.gz"
    archive="$DEVBOX_CACHE_DIR/apache-maven-$version-bin.tar.gz"
    sumfile="$archive.sha512"
    mkdir -p "$INSTALL_DIR"
    sdk download "$base" "$archive"
    sdk fetch "$base.sha512" >"$sumfile" || {
      rm -f "$archive" "$sumfile"
      sdk emit log error "checksum file not reachable: $base.sha512"
      return 3
    }
    read -r expected _ <"$sumfile"
    got=$(sha512sum "$archive")
    got=${got%% *}
    rm -f "$sumfile"
    if [ -z "$expected" ] || [ "${got,,}" != "${expected,,}" ]; then
      rm -f "$archive"
      sdk emit log error "SHA-512 of Maven $version does not match"
      return 3
    fi
    tmp=$(mktemp -d "$INSTALL_DIR/.unpack.XXXXXX")
    tar -xzf "$archive" -C "$tmp" --strip-components=1
    mv "$tmp" "$dir"
    rm -f "$archive"
    sdk emit change file create "$dir" "Maven $version"
  fi
  sdk emit step "install-$version" "done" "Maven $version"
}

# set_current VERSION: points "current" at a version and writes MAVEN_HOME and PATH.
set_current() {
  local target="$INSTALL_DIR/$1"
  if [ "$(readlink "$CURRENT" 2>/dev/null || true)" != "$target" ]; then
    is_dry_run || ln -sfn "$target" "$CURRENT"
    sdk emit change link update "$CURRENT" "points to $1"
    sdk emit notice restart-shell "New terminals use MAVEN_HOME right away, open ones after a restart."
  fi
  {
    echo "export MAVEN_HOME=\"$CURRENT\""
    echo "export PATH=\"\$MAVEN_HOME/bin:\$PATH\""
  } | sdk block "$ENV_FILE" maven
}
