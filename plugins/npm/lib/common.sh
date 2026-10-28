#!/usr/bin/env bash
# Shared functions of the npm plugin. Sourced by checks, actions and consumers.
# Uses only devbox sdk and the commands listed in requires.commands.

# shellcheck disable=SC2034 # settings are used by the scripts that source this file
sdk() { "$DEVBOX_BIN" sdk "$@"; }

NPMRC=$(sdk setting npmrc)
MODE=$(sdk setting mode)
ENV_FILE=$(sdk setting envFile)
ALWAYS_AUTH=$(sdk setting alwaysAuth)

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# shellcheck source=layout.sh
. "$DEVBOX_PLUGIN_DIR/lib/layout.sh"

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\t'/\\t}
  printf '"%s"' "$s"
}

# has_block FILE NAME: whether the marked block of NAME is in FILE.
has_block() {
  local content
  [ -f "$1" ] || return 1
  content=$(<"$1")
  [[ $content == *"# >>> devbox:$2 >>>"* ]]
}

# drop_block FILE NAME: removes the marked block; a file that is empty afterwards (so
# DevBox created it) is deleted, which leaves the system as before.
drop_block() {
  local file=$1 name=$2
  has_block "$file" "$name" || return 0
  sdk block-remove "$file" "$name"
  if ! is_dry_run && [ ! -s "$file" ]; then
    rm -f "$file"
    sdk emit change file delete "$file" "empty after removing devbox:$name"
  fi
}
