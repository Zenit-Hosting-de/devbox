#!/usr/bin/env bash
# Shared functions of the dotnet plugin. Sourced by checks and actions.
# Uses only devbox sdk, the commands listed in requires.commands, the downloaded
# dotnet-install.sh and the dotnet of the install location.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

INSTALL_DIR=$(sdk setting installDir)
INSTALL_DIR=${INSTALL_DIR%/}
METADATA=$(sdk setting metadataUrl)
SCRIPT_URL=$(sdk setting installScriptUrl)
FEED=$(sdk setting feedUrl)
FEED=${FEED%/}
CHANNEL=$(sdk setting channel)
ENV_FILE=$(sdk setting envFile)
DOTNET="$INSTALL_DIR/dotnet"

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

# dotnet_arch: the architecture name of dotnet-install.sh.
dotnet_arch() {
  case "$(sdk json-get "$DEVBOX_REQUEST" /host/arch)" in
  amd64) echo x64 ;;
  arm64) echo arm64 ;;
  *) return 3 ;;
  esac
}

# newest VERSION…: the highest of the versions.
newest() { printf '%s\n' "$@" | sed '/^$/d' | sort -V | tail -n1; }

# channel_of VERSION: the channel of an SDK version (10.0.100 -> 10.0).
channel_of() {
  [[ $1 =~ ^([0-9]+\.[0-9]+)(\.|$) ]] && echo "${BASH_REMATCH[1]}"
}

# band_of VERSION: the feature band of an SDK version (10.0.105 -> 10.0.100).
band_of() {
  [[ $1 =~ ^([0-9]+)\.([0-9]+)\.([0-9])[0-9][0-9] ]] &&
    echo "${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}00"
}

# installed_versions: installed SDK versions, newest first.
installed_versions() {
  local d
  [ -x "$DOTNET" ] && [ -d "$INSTALL_DIR/sdk" ] || return 0
  for d in "$INSTALL_DIR"/sdk/*/; do
    d=${d%/}
    [ -L "$d" ] && continue
    [ -d "$d" ] && basename "$d"
  done | sort -rV
}

# installed_match SPEC: the installed SDK SPEC (a channel or a full version) means.
installed_match() {
  local spec=$1 v
  while IFS= read -r v; do
    [ -n "$v" ] || continue
    if [ "$v" = "$spec" ] || [[ $v == "$spec".* ]]; then
      echo "$v"
      return 0
    fi
  done < <(installed_versions)
  return 1
}

# valid_spec SPEC: a channel, a full SDK version, lts, sts or latest.
valid_spec() {
  [[ $1 =~ ^(lts|sts|latest|[0-9]+(\.[0-9]+)?|[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?)$ ]]
}

# fetch_index FILE: downloads releases-index.json.
fetch_index() { sdk fetch "$METADATA" >"$1"; }

# index_rows FILE: "CHANNEL LATEST-SDK RELEASE-TYPE SUPPORT-PHASE" per channel, in the
# order of the index (newest first).
index_rows() {
  local -a ch sdk_v type phase
  local i
  mapfile -t ch < <(sdk json-get "$1" '/releases-index/*/channel-version')
  mapfile -t sdk_v < <(sdk json-get "$1" '/releases-index/*/latest-sdk')
  mapfile -t type < <(sdk json-get "$1" '/releases-index/*/release-type')
  mapfile -t phase < <(sdk json-get "$1" '/releases-index/*/support-phase')
  for i in "${!ch[@]}"; do
    printf '%s %s %s %s\n' "${ch[$i]}" "${sdk_v[$i]:-}" "${type[$i]:-}" "${phase[$i]:-}"
  done
}

# channel_sdks CHANNEL: every SDK version of a channel from CHANNEL/releases.json next
# to releases-index.json.
channel_sdks() {
  local file rc=0
  file=$(mktemp "$DEVBOX_CACHE_DIR/releases.XXXXXX")
  if sdk fetch "${METADATA%/*}/$1/releases.json" >"$file"; then
    {
      sdk json-get "$file" '/releases/*/sdk/version' || true
      sdk json-get "$file" '/releases/*/sdks/*/version' || true
    } 2>/dev/null | sort -u
  else
    rc=1
  fi
  rm -f "$file"
  return "$rc"
}

# resolve_version SPEC INDEX: the SDK version SPEC means. An empty SPEC follows the
# channel setting. Returns 1 when nothing matches, 2 for an invalid SPEC.
resolve_version() {
  local spec=$1 rows ch latest type phase
  [ -n "$spec" ] || spec=$CHANNEL
  valid_spec "$spec" || return 2
  rows=$(index_rows "$2")
  if [[ $spec =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    ch=$(channel_of "$spec")
    [[ $'\n'$rows == *$'\n'"$ch "* ]] || return 1
    while IFS= read -r latest; do
      if [ "$latest" = "$spec" ]; then
        echo "$spec"
        return 0
      fi
    done < <(channel_sdks "$ch")
    return 1
  fi
  while read -r ch latest type phase; do
    [ -n "$latest" ] || continue
    case "$spec" in
    lts | sts | latest)
      case "$phase" in preview | go-live) continue ;; esac
      [ "$spec" = latest ] || [ "$type" = "$spec" ] || continue
      ;;
    *) [ "$ch" = "$spec" ] || [[ $ch == "$spec".* ]] || continue ;;
    esac
    echo "$latest"
    return 0
  done <<<"$rows"
  return 1
}

# install_sdk VERSION: installs an SDK with dotnet-install.sh unless it is there.
install_sdk() {
  local version=$1 dir="$INSTALL_DIR/sdk/$1" arch script
  if [ -x "$DOTNET" ] && [ -d "$dir" ]; then
    sdk emit step "install-$version" skipped ".NET SDK $version"
    return 0
  fi
  sdk emit step "install-$version" running ".NET SDK $version"
  if is_dry_run; then
    sdk emit change file create "$dir" ".NET SDK $version"
  else
    arch=$(dotnet_arch)
    script="$DEVBOX_CACHE_DIR/dotnet-install.sh"
    if ! sdk fetch "$SCRIPT_URL" >"$script"; then
      rm -f "$script"
      sdk emit log error "install script not reachable: $SCRIPT_URL"
      return 3
    fi
    mkdir -p "$INSTALL_DIR"
    if ! bash "$script" --version "$version" --install-dir "$INSTALL_DIR" \
      --architecture "$arch" --azure-feed "$FEED" --no-path >&2; then
      rm -f "$script"
      sdk emit log error "dotnet-install.sh failed for SDK $version"
      return 1
    fi
    rm -f "$script"
    if ! [ -x "$DOTNET" ] || ! [ -d "$dir" ]; then
      sdk emit log error "dotnet-install.sh did not create $dir"
      return 1
    fi
    sdk emit change file create "$dir" ".NET SDK $version"
  fi
  sdk emit step "install-$version" "done" ".NET SDK $version"
}

# runtime_of VERSION: the runtime version an installed SDK runs on.
runtime_of() {
  local f="$INSTALL_DIR/sdk/$1/dotnet.runtimeconfig.json"
  [ -f "$f" ] && sdk json-get "$f" /runtimeOptions/framework/version 2>/dev/null
}

# remove_sdk VERSION [REMAINING…]: deletes an SDK and the shared runtimes no other
# SDK runs on. REMAINING are the SDKs that stay (default: the installed ones). Without
# any SDK left the whole install location and the environment block go.
remove_sdk() {
  local version=$1 rt v d left=""
  shift
  rt=$(runtime_of "$version" || true)
  while IFS= read -r v; do
    [ -n "$v" ] && [ "$v" != "$version" ] && left+="$v "
  done < <(if [ "$#" -gt 0 ]; then printf '%s\n' "$@"; else installed_versions; fi)
  if [ -z "$left" ]; then
    is_dry_run || rm -rf "${INSTALL_DIR:?}"
    sdk emit change file delete "$INSTALL_DIR" ".NET SDK $version, last SDK"
    sdk block-remove "$ENV_FILE" dotnet
    return 0
  fi
  is_dry_run || rm -rf "${INSTALL_DIR:?}/sdk/$version"
  sdk emit change file delete "$INSTALL_DIR/sdk/$version" ".NET SDK $version"
  [ -n "$rt" ] || return 0
  for v in $left; do
    [ "$(runtime_of "$v" || true)" = "$rt" ] && return 0
  done
  for d in "$INSTALL_DIR"/shared/*/"$rt"; do
    [ -d "$d" ] || continue
    is_dry_run || rm -rf "${d:?}"
    sdk emit change file delete "$d" "runtime $rt"
  done
}

# set_env: sets DOTNET_ROOT and puts dotnet in PATH of new shells.
set_env() {
  {
    echo "export DOTNET_ROOT=\"$INSTALL_DIR\""
    echo "case \":\$PATH:\" in"
    echo "*\":$INSTALL_DIR:\"*) ;;"
    echo "*) export PATH=\"$INSTALL_DIR:\$PATH\" ;;"
    echo "esac"
  } | sdk block "$ENV_FILE" dotnet
}

# workloads_of VERSION: workloads installed for the feature band of an SDK.
workloads_of() {
  local band d
  band=$(band_of "$1") || return 0
  for d in "$INSTALL_DIR"/metadata/workloads/"$band"/InstalledWorkloads/* \
    "$INSTALL_DIR"/metadata/workloads/*/"$band"/InstalledWorkloads/*; do
    [ -f "$d" ] && basename "$d"
  done | sort -u
}

# install_workloads VERSION [FROM]: installs the configured workloads into the SDK
# VERSION (the newest one, which dotnet uses by default), plus the ones the SDK FROM
# had. Workloads that are there already are left alone.
install_workloads() {
  local version=$1 from=${2:-} id
  local -a ids=()
  local -A have=() wanted=()
  while IFS= read -r id; do
    [ -n "$id" ] && have[$id]=1
  done < <(workloads_of "$version")
  while IFS= read -r id; do
    [ -n "$id" ] || continue
    [ -n "${have[$id]:-}" ] && continue
    [ -n "${wanted[$id]:-}" ] && continue
    wanted[$id]=1
    ids+=("$id")
  done < <(
    setting_list workloads
    [ -z "$from" ] || [ "$from" = "$version" ] || workloads_of "$from"
  )
  [ "${#ids[@]}" -gt 0 ] || return 0
  sdk emit step "workloads-$version" running "dotnet workload install ${ids[*]}"
  if ! is_dry_run; then
    if ! (cd "$INSTALL_DIR" && DOTNET_ROOT="$INSTALL_DIR" DOTNET_NOLOGO=1 \
      "$DOTNET" workload install "${ids[@]}" >&2); then
      sdk emit log error "dotnet workload install ${ids[*]} failed for SDK $version"
      return 1
    fi
  fi
  for id in "${ids[@]}"; do
    sdk emit change package create "$id" "workload for .NET SDK $version"
  done
  sdk emit step "workloads-$version" "done" "dotnet workload install ${ids[*]}"
}
