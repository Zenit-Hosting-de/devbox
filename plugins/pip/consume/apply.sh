#!/usr/bin/env bash
# Consumer: writes a registry credential as package index for pip and uv.
#   mode file: marked blocks with the index address in pip.conf ([global] index-url)
#              and uv.toml (default [[index]]); the credential goes into .netrc
#              (mode 0600), never into the address
#   mode env:  marked block in the shell environment file (mode 0600) that exports
#              PIP_INDEX_URL and UV_DEFAULT_INDEX with the credential in the address
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
base=$(sdk field url)
layout=$(sdk field layout)
user=$(sdk field username 2>/dev/null) || user=""
repo=$(sdk option repository 2>/dev/null) || repo=""
url=$(sdk option url 2>/dev/null) || url=""
[ -n "$user" ] || user=$(sdk setting tokenUsername)

index=$(index_url "$layout" "$base" "$repo" "$url") || exit 2
host=$(url_host "$index")
block="pip:$name"
want_pip=false
want_uv=false
! has_target pip || want_pip=true
! has_target uv || want_uv=true

fail() {
  sdk emit log error "$1"
  echo "$1" >&2
  exit "${2:-1}"
}

# Conflicts are checked before anything changes: pip refuses a second index-url in its
# configuration, and only one value of a variable takes effect.
if [ "$MODE" = env ]; then
  ! $want_pip || ! outside_matches "$ENV_FILE" "$block" '^[[:space:]]*(export[[:space:]]+)?PIP_INDEX_URL=' ||
    fail "$ENV_FILE sets PIP_INDEX_URL outside the block devbox:$block; only one index can take effect"
  ! $want_uv || ! outside_matches "$ENV_FILE" "$block" '^[[:space:]]*(export[[:space:]]+)?UV_DEFAULT_INDEX=' ||
    fail "$ENV_FILE sets UV_DEFAULT_INDEX outside the block devbox:$block; only one index can take effect"
else
  ! $want_pip || ! outside_matches "$PIP_CONF" "$block" '^[[:space:]]*index[-_]url[[:space:]]*[=:]' ||
    fail "$PIP_CONF sets index-url outside the block devbox:$block; pip accepts only one"
  if $want_pip || $want_uv; then
    netrc_safe "$user" || fail "the user name cannot be written into .netrc (empty, blank, quote or backslash); use mode env" 2
  fi
fi

sdk emit step "apply-$name" running "$index"
if [ "$MODE" = env ]; then
  secret=$(sdk secret)
  if $want_pip || $want_uv; then
    # Built in shell variables and piped with the printf builtin, so the secret never
    # appears in the arguments of a process or in a temporary file.
    auth_url=$(url_with_auth "$index" "$user" "$secret")
    text=""
    ! $want_pip || text+="export PIP_INDEX_URL=$(shell_quote "$auth_url")"$'\n'
    ! $want_uv || text+="export UV_DEFAULT_INDEX=$(shell_quote "$auth_url")"$'\n'
    is_dry_run || { [ ! -e "$ENV_FILE" ] || chmod 600 "$ENV_FILE"; }
    mark_created "$ENV_FILE" 600
    printf '%s' "$text" | sdk block "$ENV_FILE" "$block" --mode 600
  else
    drop_block "$ENV_FILE" "$block"
  fi
  drop_block "$PIP_CONF" "$block"
  drop_block "$UV_CONF" "$block"
  drop_block "$NETRC" "$block"
else
  if $want_pip; then
    if outside_matches "$PIP_CONF" "$block" '^[[:space:]]*\[global\][[:space:]]*$'; then
      # The file has a [global] section of its own: the block goes into it.
      before=$(global_insert_point "$PIP_CONF" "$block")
      printf 'index-url = %s\n' "$index" | put_block "$PIP_CONF" "$block" 644 "$before"
    else
      mark_created "$PIP_CONF"
      printf '[global]\nindex-url = %s\n' "$index" | put_block "$PIP_CONF" "$block" 644
    fi
  else
    drop_block "$PIP_CONF" "$block"
  fi
  if $want_uv; then
    if outside_matches "$UV_CONF" "$block" '^[[:space:]]*default[[:space:]]*=[[:space:]]*true'; then
      sdk emit log warn "$UV_CONF has a default index of its own before this one; uv uses the first"
    fi
    mark_created "$UV_CONF"
    printf '[[index]]\nname = "%s"\nurl = "%s"\ndefault = true\n' "$name" "$index" | put_block "$UV_CONF" "$block" 644
  else
    drop_block "$UV_CONF" "$block"
  fi
  if $want_pip || $want_uv; then
    secret=$(sdk secret)
    netrc_safe "$secret" || fail "the secret cannot be written into .netrc (blank, quote or backslash); use mode env" 2
    if outside_matches "$NETRC" "$block" "^[[:space:]]*machine[[:space:]]+${host//./\\.}([[:space:]]|$)"; then
      sdk emit log warn "$NETRC has an entry of its own for $host; tools may use either"
    fi
    is_dry_run || { [ ! -e "$NETRC" ] || chmod 600 "$NETRC"; }
    mark_created "$NETRC" 600
    printf 'machine %s login %s password %s\n' "$host" "$user" "$secret" |
      put_block "$NETRC" "$block" 600 "$(insert_point "$NETRC")"
  else
    drop_block "$NETRC" "$block"
  fi
  drop_block "$ENV_FILE" "$block"
fi
$want_pip || $want_uv || sdk emit log warn "the setting targets names no tool; nothing written"
sdk emit step "apply-$name" "done" "$index"
sdk emit result "Index $index written (mode $MODE)"
