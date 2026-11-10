#!/usr/bin/env bash
# Shared functions of the python plugin. Sourced by checks and actions.
# Uses only devbox sdk, the commands listed in requires.commands and the uv that the
# plugin installs itself.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

INSTALL_URL=$(sdk setting uvInstallUrl)
MIRROR=$(sdk setting pythonMirror)
BIN_DIR=$(sdk setting binDir)
PYTHON_DIR=$(sdk setting pythonDir)
TOOL_DIR=$(sdk setting toolDir)
ENV_FILE=$(sdk setting envFile)
UV="$BIN_DIR/uv"

# Where uv puts Python, tools and their commands. Exported so every uv call (and the
# installer) uses the configured locations.
export UV_INSTALL_DIR="$BIN_DIR" UV_NO_MODIFY_PATH=1 INSTALLER_NO_MODIFY_PATH=1
export UV_PYTHON_INSTALL_DIR="$PYTHON_DIR" UV_PYTHON_BIN_DIR="$BIN_DIR"
export UV_TOOL_DIR="$TOOL_DIR" UV_TOOL_BIN_DIR="$BIN_DIR"
export UV_NO_PROGRESS=1
if [ -n "$MIRROR" ]; then
  export UV_PYTHON_INSTALL_MIRROR="$MIRROR"
fi

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# Read-only calls (checks, dry runs) must not create uv's cache in the home directory.
if is_dry_run || [ -z "${DEVBOX_SESSION_ID:-}" ]; then
  export UV_NO_CACHE=1
fi

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# setting_list KEY: the items of a string array setting, one per line. Read from the
# request, so items may contain commas and spaces (black>=24,<25).
setting_list() {
  sdk json-get "$DEVBOX_REQUEST" "/settings/$1/*" 2>/dev/null || true
}

# run_uv ARGS…: runs the installed uv; its output goes to the terminal.
run_uv() { "$UV" "$@" >&2; }

has_uv() { [ -x "$UV" ]; }

# uv_version: the version of the installed uv (uv 0.5.1 (…) -> 0.5.1).
uv_version() {
  local out
  out=$("$UV" --version 2>/dev/null) || return 0
  out=${out#uv }
  echo "${out%% *}"
}

# minor VERSION: the minor version (3.12.7 -> 3.12).
minor() {
  local rest=${1#*.}
  echo "${1%%.*}.${rest%%.*}"
}

# newer A B: succeeds when version A is newer than B.
newer() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1)" = "$1" ]; }

# matches SPEC VERSION: VERSION is SPEC or a patch release of it.
matches() { [ "$2" = "$1" ] || [[ $2 == "$1".* ]]; }

# valid_spec SPEC: a minor or full version.
valid_spec() { [[ $1 =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; }

# installed_list: "VERSION PATH" of the Python versions uv manages, newest first.
installed_list() {
  local json
  local -a versions paths
  local i
  has_uv || return 0
  json=$("$UV" python list --only-installed --managed-python --output-format json 2>/dev/null) || return 0
  mapfile -t versions < <(sdk json-get - '/*/version' <<<"$json" 2>/dev/null || true)
  mapfile -t paths < <(sdk json-get - '/*/path' <<<"$json" 2>/dev/null || true)
  for i in "${!versions[@]}"; do
    printf '%s %s\n' "${versions[$i]}" "${paths[$i]:-}"
  done | sort -rV -k1,1 -u
}

# installed_versions: installed Python versions, newest first.
installed_versions() {
  local v _p
  while read -r v _p; do
    [ -n "$v" ] && echo "$v"
  done < <(installed_list)
  return 0
}

# installed_match SPEC: the newest installed version SPEC means.
installed_match() {
  local v
  while IFS= read -r v; do
    if matches "$1" "$v"; then
      echo "$v"
      return 0
    fi
  done < <(installed_versions)
  return 1
}

# available_versions: stable CPython releases uv can download (the newest patch of
# each minor version), newest first.
available_versions() {
  local json
  local -a versions impls variants
  local i
  has_uv || return 0
  json=$("$UV" python list --only-downloads --managed-python --output-format json 2>/dev/null) || return 0
  mapfile -t versions < <(sdk json-get - '/*/version' <<<"$json" 2>/dev/null || true)
  mapfile -t impls < <(sdk json-get - '/*/implementation' <<<"$json" 2>/dev/null || true)
  mapfile -t variants < <(sdk json-get - '/*/variant' <<<"$json" 2>/dev/null || true)
  for i in "${!versions[@]}"; do
    [ "${impls[$i]:-}" = cpython ] || continue
    [ "${variants[$i]:-default}" = default ] || continue
    [[ ${versions[$i]} =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
    echo "${versions[$i]}"
  done | sort -rV -u
}

# available_match SPEC: the newest downloadable release SPEC means. An empty SPEC
# means the newest stable release.
available_match() {
  local v
  while IFS= read -r v; do
    if [ -z "$1" ] || matches "$1" "$v"; then
      echo "$v"
      return 0
    fi
  done < <(available_versions)
  return 1
}

# default_version: the version behind python in BIN_DIR, if any.
default_version() {
  local out
  [ -x "$BIN_DIR/python" ] || return 0
  out=$("$BIN_DIR/python" --version 2>&1) || return 0
  out=${out#Python }
  echo "${out%% *}"
}

# install_uv: runs the uv installer unless uv is there already.
install_uv() {
  local script
  if has_uv; then
    sdk emit step uv skipped "uv"
    return 0
  fi
  sdk emit step uv running "uv"
  if ! is_dry_run; then
    mkdir -p "$BIN_DIR"
    script="$DEVBOX_CACHE_DIR/uv-install.sh"
    if ! sdk download "$INSTALL_URL" "$script"; then
      sdk emit log error "uv installer not reachable: $INSTALL_URL"
      return 3
    fi
    if ! sh "$script" >&2; then
      rm -f "$script"
      sdk emit log error "uv installer failed: $INSTALL_URL"
      return 1
    fi
    rm -f "$script"
    if ! has_uv; then
      sdk emit log error "uv installer did not create $UV"
      return 1
    fi
  fi
  sdk emit change file create "$UV" "uv"
  sdk emit step uv "done" "uv"
}

# write_env: puts BIN_DIR in PATH of new shells.
write_env() {
  {
    echo "case \":\$PATH:\" in"
    echo "*\":$BIN_DIR:\"*) ;;"
    echo "*) export PATH=\"$BIN_DIR:\$PATH\" ;;"
    echo "esac"
  } | sdk block "$ENV_FILE" python
}

# resolve_spec SPEC: SPEC itself, or for an empty SPEC the minor version of the newest
# stable release uv knows (empty when uv is not installed yet).
resolve_spec() {
  local v
  if [ -n "$1" ]; then
    echo "$1"
  elif has_uv && v=$(available_match ""); then
    minor "$v"
  fi
  return 0
}

# install_python SPEC: installs the newest release SPEC means unless a matching version
# is installed already. SPEC is empty only in a dry run before uv is installed.
install_python() {
  local spec=$1 label=${1:-newest release} have version
  if [ -n "$spec" ] && have=$(installed_match "$spec"); then
    sdk emit step "python-$spec" skipped "Python $have"
    return 0
  fi
  sdk emit step "python-${spec:-newest}" running "Python $label"
  if is_dry_run; then
    version=$(available_match "$spec") || version=$label
  elif [ -z "$spec" ]; then
    sdk emit log error "uv knows no stable Python release"
    return 3
  else
    if ! run_uv python install "$spec"; then
      sdk emit log error "uv python install $spec failed"
      return 1
    fi
    version=$(installed_match "$spec") || {
      sdk emit log error "Python $spec is missing after the installation"
      return 1
    }
  fi
  sdk emit change file create "$PYTHON_DIR" "Python $version"
  sdk emit step "python-${spec:-newest}" "done" "Python $label"
}

# set_default SPEC: makes the installed version SPEC means the one behind python and
# python3 in BIN_DIR.
set_default() {
  local spec=$1 version target
  version=$(installed_match "$spec") || version=""
  if [ -z "$version" ] && ! is_dry_run; then
    sdk emit log error "Python $spec is not installed"
    return 2
  fi
  target=${version:-${spec:-newest release}}
  [ -n "$version" ] && [ "$(default_version)" = "$version" ] && return 0
  if ! is_dry_run; then
    if ! run_uv python install --default "$version"; then
      sdk emit log error "uv python install --default $version failed"
      return 1
    fi
  fi
  sdk emit change link update "$BIN_DIR/python" "points to Python $target"
  sdk emit notice restart-shell "New terminals use the default Python right away, open ones after a restart."
}

# tool_name SPEC: the normalised package name of a tool spec (black==24.1 -> black,
# Ruff[extra]>=0.5 -> ruff, my_tool -> my-tool).
tool_name() {
  local name=$1
  [[ $1 =~ ^([A-Za-z0-9._-]+) ]] && name=${BASH_REMATCH[1]}
  name=${name,,}
  name=${name//_/-}
  echo "${name//./-}"
}

# tool_lines: "NAME VERSION" of the tools installed with uv tool.
tool_lines() {
  local name version _rest
  has_uv || return 0
  while read -r name version _rest; do
    case "$name" in "" | -*) continue ;; esac
    echo "$(tool_name "$name") ${version#v}"
  done < <("$UV" tool list 2>/dev/null || true)
}

# install_tools: installs the configured tools that are missing.
install_tools() {
  local spec name _v
  local -A have=()
  while read -r name _v; do
    [ -n "$name" ] && have[$name]=1
  done < <(tool_lines)
  while IFS= read -r spec; do
    [ -n "$spec" ] || continue
    name=$(tool_name "$spec")
    [ -n "${have[$name]:-}" ] && continue
    have[$name]=1
    sdk emit step "tool-$name" running "uv tool install $spec"
    if ! is_dry_run; then
      if ! run_uv tool install "$spec"; then
        sdk emit log error "uv tool install $spec failed"
        return 1
      fi
    fi
    sdk emit change package create "$name" "Python tool"
    sdk emit step "tool-$name" "done" "uv tool install $spec"
  done < <(setting_list tools)
}
