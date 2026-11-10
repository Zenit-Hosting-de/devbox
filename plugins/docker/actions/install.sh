#!/usr/bin/env bash
# Action: install Docker Engine from the packages of the distribution or, with the
# source docker, from the package repository of Docker, which is set up first.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

pm=$(package_manager)
[ -n "$pm" ] || fail "$no_manager_text" 3
source=$(sdk setting source)
service=$(sdk setting service)
mapfile -t wanted < <(wanted_packages "$pm")
[ "${#wanted[@]}" -gt 0 ] || fail "No packages are configured for $pm and the source $source" 2
if [ "$source" = docker ]; then
  case "$pm" in
  apt | dnf) ;;
  *) fail "The package repository of Docker is supported with apt and dnf, not with $pm; use the source distro" 3 ;;
  esac
fi

# setup_repository: writes the source of the Docker repository for the package manager.
# Sets repo_changed when something was (or would be) written.
repo_changed=false
setup_repository() {
  local url key_url channel
  url=$(sdk setting repoUrl)
  key_url=$(sdk setting repoKeyUrl)
  channel=$(sdk setting repoChannel)
  [ -n "$url" ] || fail "The setting repoUrl is empty" 2
  stage_dir
  if [ "$pm" = apt ]; then
    local key_file source_file suite arch
    key_file=$(sdk setting aptKeyFile)
    source_file=$(sdk setting aptSourceFile)
    suite=$(sdk setting repoSuite)
    arch=$(sdk json-get "$DEVBOX_REQUEST" /host/arch)
    [ -n "$suite" ] || fail "The setting repoSuite is empty: the code name of the distribution is unknown" 2
    if [ ! -e "$key_file" ]; then
      [ -n "$key_url" ] || fail "The setting repoKeyUrl is empty" 2
      if is_dry_run; then
        sdk emit change file create "$key_file" "download $key_url"
      else
        stage_sdk download "$key_url" "$STAGE/key"
        put_file "$STAGE/key" "$key_file" 644 "downloaded $key_url"
      fi
      repo_changed=true
    fi
    printf '%s\n' \
      "# Written by DevBox (plugin docker). Changes are overwritten." \
      "Types: deb" \
      "URIs: $url" \
      "Suites: $suite" \
      "Components: $channel" \
      "Architectures: $arch" \
      "Signed-By: $key_file" >"$STAGE/source"
    if differs "$STAGE/source" "$source_file"; then
      put_file "$STAGE/source" "$source_file" 644 "package repository $url"
      repo_changed=true
    fi
  else
    local repo_file
    repo_file=$(sdk setting rpmRepoFile)
    printf '%s\n' \
      "# Written by DevBox (plugin docker). Changes are overwritten." \
      "[devbox-docker]" \
      "name=Docker (DevBox)" \
      "baseurl=$(sdk setting rpmBaseUrl)" \
      "enabled=1" \
      "gpgcheck=1" \
      "gpgkey=$key_url" >"$STAGE/repo"
    if differs "$STAGE/repo" "$repo_file"; then
      put_file "$STAGE/repo" "$repo_file" 644 "package repository $url"
      repo_changed=true
    fi
  fi
}

# service_ready: whether the service needs no attention: enabling is switched off,
# systemctl does not exist, or the service is enabled and running.
service_ready() {
  [ "$(sdk setting enableService)" = true ] || return 0
  command -v systemctl >/dev/null 2>&1 || return 0
  systemctl is-enabled --quiet "$service" 2>/dev/null && systemctl is-active --quiet "$service" 2>/dev/null
}

[ "$source" != docker ] || setup_repository

missing=()
for p in "${wanted[@]}"; do
  installed_version "$pm" "$p" >/dev/null || missing+=("$p")
done
for p in "${missing[@]}"; do
  sdk emit change package create "$p" "install with $pm"
done
enable=false
if ! service_ready; then
  enable=true
  sdk emit change service update "$service" "enable and start"
fi

if [ "${#missing[@]}" -eq 0 ] && ! $repo_changed && ! $enable; then
  sdk emit result "Docker Engine is installed"
  exit 0
fi
if is_dry_run; then
  sdk emit result "${#missing[@]} packages would be installed"
  exit 0
fi

if [ "${#missing[@]}" -gt 0 ] || $repo_changed; then
  sdk emit step install running "Install Docker Engine"
  case "$pm" in
  apt)
    as_root apt-get update
    [ "${#missing[@]}" -eq 0 ] || as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
    ;;
  dnf) [ "${#missing[@]}" -eq 0 ] || as_root dnf install -y "${missing[@]}" ;;
  pacman) as_root pacman -S --needed --noconfirm "${missing[@]}" ;;
  zypper) as_root zypper --non-interactive install "${missing[@]}" ;;
  esac
  sdk emit step install "done" "Install Docker Engine"
fi

still=()
for p in "${wanted[@]}"; do
  installed_version "$pm" "$p" >/dev/null || still+=("$p")
done
[ "${#still[@]}" -eq 0 ] || fail "Still missing: ${still[*]}"

if $enable && ! as_root systemctl enable --now "$service"; then
  sdk emit log warn "The service $service could not be enabled and started"
fi
sdk emit result "Docker Engine installed (${#wanted[@]} packages, source $source)" "{\"installed\":$(json_list "${missing[@]}")}"
