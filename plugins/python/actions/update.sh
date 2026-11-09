#!/usr/bin/env bash
# Action: update uv itself, every installed minor version of Python to its newest
# patch release and every tool installed with uv tool.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if ! has_uv; then
  sdk emit result "uv is not installed"
  exit 0
fi
updated=""

# uv itself
if [ "$(sdk setting selfUpdate)" = true ]; then
  before=$(uv_version)
  sdk emit step uv running "uv self update"
  if is_dry_run; then
    if out=$("$UV" self update --dry-run 2>&1); then
      if [[ $out == *"Would update"* ]]; then
        sdk emit change file update "$UV" "newer uv"
      fi
    else
      sdk emit log warn "uv self update --dry-run failed"
    fi
  elif run_uv self update; then
    after=$(uv_version)
    if [ "$after" != "$before" ]; then
      sdk emit change file update "$UV" "uv $after"
      updated+="${updated:+, }uv $after"
    fi
  else
    sdk emit log warn "uv self update failed; uv is kept as it is"
  fi
  sdk emit step uv "done" "uv self update"
fi

# Python: the newest patch release of every installed minor version
mapfile -t versions < <(installed_versions)
if [ "${#versions[@]}" -gt 0 ]; then
  sdk emit step python running "uv python upgrade"
  if is_dry_run; then
    declare -A seen=()
    for v in "${versions[@]}"; do
      m=$(minor "$v")
      [ -n "${seen[$m]:-}" ] && continue
      seen[$m]=1
      latest=$(available_match "$m") || continue
      if newer "$latest" "$v"; then
        sdk emit change file create "$PYTHON_DIR" "Python $latest"
      fi
    done
  else
    if ! run_uv python upgrade; then
      sdk emit log error "uv python upgrade failed"
      exit 1
    fi
    declare -A had=()
    for v in "${versions[@]}"; do had[$v]=1; done
    while IFS= read -r v; do
      [ -n "$v" ] && [ -z "${had[$v]:-}" ] || continue
      sdk emit change file create "$PYTHON_DIR" "Python $v"
      updated+="${updated:+, }Python $v"
    done < <(installed_versions)
  fi
  sdk emit step python "done" "uv python upgrade"
fi

# tools installed with uv tool
mapfile -t tools_before < <(tool_lines)
if [ "${#tools_before[@]}" -gt 0 ]; then
  sdk emit step tools running "uv tool upgrade --all"
  if is_dry_run; then
    while read -r name _version latest; do
      case "$name" in "" | -*) continue ;; esac
      sdk emit change package update "$(tool_name "$name")" "Python tool ${latest:-newer release}"
    done < <("$UV" tool list --outdated 2>/dev/null || true)
  else
    if ! run_uv tool upgrade --all; then
      sdk emit log error "uv tool upgrade --all failed"
      exit 1
    fi
    declare -A old=()
    for line in "${tools_before[@]}"; do old[${line%% *}]=${line#* }; done
    while read -r name version; do
      [ -n "$name" ] || continue
      if [ "${old[$name]:-}" != "$version" ]; then
        sdk emit change package update "$name" "Python tool $version"
        updated+="${updated:+, }$name $version"
      fi
    done < <(tool_lines)
  fi
  sdk emit step tools "done" "uv tool upgrade --all"
fi

sdk emit result "${updated:-Up to date}"
