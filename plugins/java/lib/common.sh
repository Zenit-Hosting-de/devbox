#!/usr/bin/env bash
# Shared functions of the java plugin. Sourced by checks and actions.
# Uses only devbox sdk and the commands listed in requires.commands.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

INSTALL_DIR=$(sdk setting installDir)
API=$(sdk setting vendorApi)
ENV_FILE=$(sdk setting envFile)
CURRENT="$INSTALL_DIR/current"

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# api_arch: the architecture name of the Adoptium API for this host.
api_arch() {
  case "$(sdk json-get "$DEVBOX_REQUEST" /host/arch)" in
  amd64) echo x64 ;;
  arm64) echo aarch64 ;;
  *) return 3 ;;
  esac
}

# installed_versions: installed JDK versions, newest first.
installed_versions() {
  local d
  [ -d "$INSTALL_DIR" ] || return 0
  for d in "$INSTALL_DIR"/*/; do
    d=${d%/}
    [ -L "$d" ] && continue
    [ -x "$d/bin/java" ] && basename "$d"
  done | sort -rV
}

# default_version: the version "current" points to, if any.
default_version() {
  local target
  target=$(readlink "$CURRENT" 2>/dev/null) || return 0
  [ -x "$target/bin/java" ] && basename "$target"
  return 0
}

# major VERSION: the feature release of a version (21.0.5 -> 21, 8.0.432 -> 8).
major() { echo "${1%%.*}"; }

# fetch_latest FEATURE FILE: release metadata of the newest build of a feature release.
fetch_latest() {
  local arch
  arch=$(api_arch)
  sdk fetch "$API/assets/latest/$1/hotspot?architecture=$arch&image_type=jdk&os=linux&vendor=eclipse" >"$2"
}

# meta_version FILE: "major.minor.security" of fetched metadata.
meta_version() {
  printf '%s.%s.%s' "$(sdk json-get "$1" /0/version/major)" "$(sdk json-get "$1" /0/version/minor)" "$(sdk json-get "$1" /0/version/security)"
}

# install_feature FEATURE: installs the newest build of a feature release unless it is
# there already, and prints the installed version.
install_feature() {
  local feature=$1 meta version dir archive tmp
  meta=$(mktemp "$DEVBOX_CACHE_DIR/latest.XXXXXX")
  fetch_latest "$feature" "$meta" || {
    rm -f "$meta"
    sdk emit log error "release API not reachable: $API"
    return 3
  }
  version=$(meta_version "$meta")
  dir="$INSTALL_DIR/$version"
  if [ -x "$dir/bin/java" ]; then
    sdk emit step "install-$version" skipped "Temurin $version"
  else
    sdk emit step "install-$version" running "Temurin $version"
    if is_dry_run; then
      sdk emit change file create "$dir" "JDK $version"
    else
      archive="$DEVBOX_CACHE_DIR/temurin-$version.tar.gz"
      sdk download "$(sdk json-get "$meta" /0/binary/package/link)" "$archive" --sha256 "$(sdk json-get "$meta" /0/binary/package/checksum)"
      tmp=$(mktemp -d "$INSTALL_DIR/.unpack.XXXXXX")
      tar -xzf "$archive" -C "$tmp" --strip-components=1
      mv "$tmp" "$dir"
      rm -f "$archive"
      sdk emit change file create "$dir" "JDK $version"
    fi
    sdk emit step "install-$version" "done" "Temurin $version"
  fi
  rm -f "$meta"
  echo "$version"
}

# set_default VERSION: points "current" and JAVA_HOME at a version.
set_default() {
  local target="$INSTALL_DIR/$1"
  if [ "$(readlink "$CURRENT" 2>/dev/null || true)" != "$target" ]; then
    is_dry_run || ln -sfn "$target" "$CURRENT"
    sdk emit change link update "$CURRENT" "points to $1"
    sdk emit notice restart-shell "New terminals use JAVA_HOME right away, open ones after a restart."
  fi
  {
    echo "export JAVA_HOME=\"$CURRENT\""
    echo "export PATH=\"\$JAVA_HOME/bin:\$PATH\""
  } | sdk block "$ENV_FILE" java
}
