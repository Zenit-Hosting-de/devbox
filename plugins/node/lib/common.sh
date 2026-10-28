#!/usr/bin/env bash
# Shared functions of the node plugin. Sourced by checks and actions.
# Uses only devbox sdk, the commands listed in requires.commands and the npm of the
# Node.js version that is being set up.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

INSTALL_DIR=$(sdk setting installDir)
DIST=$(sdk setting distUrl)
DIST=${DIST%/}
CHANNEL=$(sdk setting channel)
ENV_FILE=$(sdk setting envFile)
CURRENT="$INSTALL_DIR/current"

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# setting_list KEY: the items of a string array setting, one per line.
setting_list() {
  # echo ends the last item with a newline, so two lists never run together
  { sdk setting "$1" | tr -d '[]"\n ' | tr ',' '\n'; echo; } | sed '/^$/d'
}

# dist_arch: the platform name in the archive names of the distribution server.
dist_arch() {
  case "$(sdk json-get "$DEVBOX_REQUEST" /host/arch)" in
  amd64) echo linux-x64 ;;
  arm64) echo linux-arm64 ;;
  *) return 3 ;;
  esac
}

# installed_versions: installed Node.js versions, newest first.
installed_versions() {
  local d
  [ -d "$INSTALL_DIR" ] || return 0
  for d in "$INSTALL_DIR"/*/; do
    d=${d%/}
    [ -L "$d" ] && continue
    [ -x "$d/bin/node" ] && basename "$d"
  done | sort -rV
}

# default_version: the version "current" points to, if any.
default_version() {
  local target
  target=$(readlink "$CURRENT" 2>/dev/null) || return 0
  [ -x "$target/bin/node" ] && basename "$target"
  return 0
}

# major VERSION: the major version (22.11.0 -> 22).
major() { echo "${1%%.*}"; }

# fetch_index FILE: downloads the release index of the distribution server.
fetch_index() { sdk fetch "$DIST/index.json" >"$1"; }

# index_pairs FILE: "VERSION LTS" per release, newest first. LTS is false or the
# codename of the long-term support line.
index_pairs() {
  local -a versions lts
  local i
  mapfile -t versions < <(sdk json-get "$1" '/*/version')
  mapfile -t lts < <(sdk json-get "$1" '/*/lts')
  for i in "${!versions[@]}"; do
    printf '%s %s\n' "${versions[$i]#v}" "${lts[$i]:-false}"
  done
}

# valid_spec SPEC: a major, minor or full version, lts or current.
valid_spec() { [[ $1 =~ ^([0-9]+(\.[0-9]+){0,2}|lts|current)$ ]]; }

# resolve_version SPEC INDEX: the newest release matching SPEC. An empty SPEC follows
# the channel setting. Returns 1 when nothing matches, 2 for an invalid SPEC.
resolve_version() {
  local spec=${1#v} pairs ver lts
  [ -n "$spec" ] || spec=$CHANNEL
  valid_spec "$spec" || return 2
  pairs=$(index_pairs "$2")
  while read -r ver lts; do
    case "$spec" in
    lts) [ "$lts" != false ] || continue ;;
    current) ;;
    *.*.*) [ "$ver" = "$spec" ] || continue ;;
    *) case "$ver" in "$spec".*) ;; *) continue ;; esac ;;
    esac
    echo "$ver"
    return 0
  done <<<"$pairs"
  return 1
}

# installed_match SPEC: the installed version SPEC (a major or full version) means.
installed_match() {
  local spec=${1#v} v
  while IFS= read -r v; do
    [ -n "$v" ] || continue
    if [ "$v" = "$spec" ] || [[ $v == "$spec".* ]]; then
      echo "$v"
      return 0
    fi
  done < <(installed_versions)
  return 1
}

# checksum_of VERSION FILENAME: the SHA-256 sum of an archive from SHASUMS256.txt.
checksum_of() {
  local sums sum name
  sums=$(sdk fetch "$DIST/v$1/SHASUMS256.txt") || return 3
  while read -r sum name; do
    if [ "${name#\*}" = "$2" ]; then
      echo "$sum"
      return 0
    fi
  done <<<"$sums"
  return 3
}

# install_version VERSION: unpacks a release into INSTALL_DIR/VERSION unless it is
# there already. The archive is verified against SHASUMS256.txt.
install_version() {
  local version=$1 dir="$INSTALL_DIR/$1" platform file sum archive tmp
  if [ -x "$dir/bin/node" ]; then
    sdk emit step "install-$version" skipped "Node.js $version"
    return 0
  fi
  sdk emit step "install-$version" running "Node.js $version"
  if is_dry_run; then
    sdk emit change file create "$dir" "Node.js $version"
  else
    platform=$(dist_arch)
    file="node-v$version-$platform.tar.gz"
    sum=$(checksum_of "$version" "$file") || {
      sdk emit log error "no checksum for $file in $DIST/v$version/SHASUMS256.txt"
      return 3
    }
    mkdir -p "$INSTALL_DIR"
    archive="$DEVBOX_CACHE_DIR/$file"
    sdk download "$DIST/v$version/$file" "$archive" --sha256 "$sum"
    tmp=$(mktemp -d "$INSTALL_DIR/.unpack.XXXXXX")
    tar -xzf "$archive" -C "$tmp" --strip-components=1
    rm -rf "${dir:?}"
    mv "$tmp" "$dir"
    rm -f "$archive"
    sdk emit change file create "$dir" "Node.js $version"
  fi
  sdk emit step "install-$version" "done" "Node.js $version"
}

# set_default VERSION: points "current" at a version and puts it in PATH of new shells.
set_default() {
  local target="$INSTALL_DIR/$1"
  if [ "$(readlink "$CURRENT" 2>/dev/null || true)" != "$target" ]; then
    is_dry_run || ln -sfn "$target" "$CURRENT"
    sdk emit change link update "$CURRENT" "points to $1"
    sdk emit notice restart-shell "New terminals use Node.js right away, open ones after a restart."
  fi
  {
    echo "case \":\$PATH:\" in"
    echo "*\":$CURRENT/bin:\"*) ;;"
    echo "*) export PATH=\"$CURRENT/bin:\$PATH\" ;;"
    echo "esac"
  } | sdk block "$ENV_FILE" node
}

# package_name SPEC: the package name of an npm spec (typescript@5 -> typescript,
# @scope/pkg@1.2 -> @scope/pkg).
package_name() {
  local spec=$1 scope=""
  if [ "${spec#@}" != "$spec" ]; then
    scope=@
    spec=${spec#@}
  fi
  echo "$scope${spec%%@*}"
}

# global_names DIR: names of the packages installed globally in a Node.js directory,
# without the ones that come with Node.js itself.
global_names() {
  local modules="$1/lib/node_modules" p s
  [ -d "$modules" ] || return 0
  for p in "$modules"/*/; do
    p=${p%/}
    case "$(basename "$p")" in
    npm | corepack | '*') continue ;;
    @*)
      for s in "$p"/*/; do
        s=${s%/}
        [ -d "$s" ] && echo "$(basename "$p")/$(basename "$s")"
      done
      ;;
    *) basename "$p" ;;
    esac
  done
}

# run_npm DIR ARGS…: runs the npm of a Node.js directory.
run_npm() {
  local dir=$1
  shift
  PATH="$dir/bin:$PATH" npm_config_fund=false npm_config_audit=false npm_config_update_notifier=false \
    "$dir/bin/npm" "$@" >&2
}

# install_globals VERSION [FROM]: installs the configured global packages into a
# Node.js version, plus the ones already installed in the version FROM (an older patch
# of the same major version). Packages that are there already are left alone.
install_globals() {
  local version=$1 from=${2:-} dir="$INSTALL_DIR/$1" spec name
  local -a specs=()
  local -A wanted=()
  while IFS= read -r spec; do
    [ -n "$spec" ] || continue
    specs+=("$spec")
    wanted[$(package_name "$spec")]=1
  done < <(setting_list globalPackages)
  if [ -n "$from" ] && [ "$from" != "$version" ]; then
    while IFS= read -r name; do
      [ -n "$name" ] || continue
      [ -n "${wanted[$name]:-}" ] && continue
      specs+=("$name")
      wanted[$name]=1
    done < <(global_names "$INSTALL_DIR/$from")
  fi
  for spec in "${specs[@]}"; do
    name=$(package_name "$spec")
    [ -d "$dir/lib/node_modules/$name" ] && continue
    sdk emit step "global-$name" running "npm install -g $spec"
    if ! is_dry_run; then
      if ! run_npm "$dir" install -g "$spec"; then
        sdk emit log error "npm install -g $spec failed in Node.js $version"
        return 1
      fi
    fi
    sdk emit change package create "$name" "global package for Node.js $version"
    sdk emit step "global-$name" "done" "npm install -g $spec"
  done
}
