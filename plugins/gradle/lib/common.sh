#!/usr/bin/env bash
# Shared functions of the gradle plugin. Sourced by checks and consumers.
# Uses only devbox sdk and the commands listed in requires.commands.

# shellcheck disable=SC2034 # settings are used by the scripts that source this file
sdk() { "$DEVBOX_BIN" sdk "$@"; }

PROPS=$(sdk setting propertiesPath)
MODE=$(sdk setting mode)
ENV_FILE=$(sdk setting envFile)
PREFIX=$(sdk setting propertyPrefix)

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# shellcheck source=names.sh
. "$DEVBOX_PLUGIN_DIR/lib/names.sh"

fail() {
  sdk emit log error "$1"
  exit 2
}

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

CREATED_BLOCK="gradle-created"

# mark_created FILE: when FILE does not exist yet, records in a marked block of its
# own that DevBox creates it (mode 0600). Call it before the first block is written.
mark_created() {
  [ ! -e "$1" ] || return 0
  printf 'File created by DevBox.\n' | sdk block "$1" "$CREATED_BLOCK" --mode 600
}

# only_created_mark FILE: whether FILE holds nothing but the creation mark (and blanks).
only_created_mark() {
  local content rest
  has_block "$1" "$CREATED_BLOCK" || return 1
  content=$(<"$1")
  rest=${content/"# >>> devbox:$CREATED_BLOCK >>>"*"# <<< devbox:$CREATED_BLOCK <<<"/}
  [[ -z ${rest//[[:space:]]/} ]]
}

# drop_block FILE NAME: removes the marked block. A file that DevBox created (it carries
# the creation mark) and that holds nothing else afterwards is deleted; a file that
# existed before stays, even when it is empty, so the system is as before.
drop_block() {
  local file=$1 name=$2
  has_block "$file" "$name" || return 0
  sdk block-remove "$file" "$name"
  if ! is_dry_run && only_created_mark "$file"; then
    rm -f "$file"
    sdk emit change file delete "$file" "created by devbox, empty after removing devbox:$name"
  fi
}

# secure FILE: a file with a secret may only be read by its owner.
secure() {
  is_dry_run || { [ ! -e "$1" ] || chmod 600 "$1"; }
}
