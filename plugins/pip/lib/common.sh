#!/usr/bin/env bash
# Shared functions of the pip plugin. Sourced by checks and consumers.
# Uses only devbox sdk and the commands listed in requires.commands.

# shellcheck disable=SC2034 # settings are used by the scripts that source this file
sdk() { "$DEVBOX_BIN" sdk "$@"; }

MODE=$(sdk setting mode)
PIP_CONF=$(sdk setting pipConfig)
UV_CONF=$(sdk setting uvConfig)
NETRC=$(sdk setting netrc)
ENV_FILE=$(sdk setting envFile)
TARGETS=()
while IFS= read -r t; do
  [ -z "$t" ] || TARGETS+=("$t")
done < <(sdk json-get "$DEVBOX_REQUEST" '/settings/targets/*' 2>/dev/null || true)

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# shellcheck source=layout.sh
. "$DEVBOX_PLUGIN_DIR/lib/layout.sh"

# has_target NAME: whether the setting targets contains NAME.
has_target() {
  local t
  for t in "${TARGETS[@]}"; do [ "$t" != "$1" ] || return 0; done
  return 1
}

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\t'/\\t}
  printf '"%s"' "$s"
}

# trim TEXT: TEXT without surrounding white space.
trim() {
  local s=$1
  s=${s#"${s%%[![:space:]]*}"}
  s=${s%"${s##*[![:space:]]}"}
  printf '%s' "$s"
}

# has_block FILE NAME: whether the marked block of NAME is in FILE.
has_block() {
  local content
  [ -f "$1" ] || return 1
  content=$(<"$1")
  [[ $content == *"# >>> devbox:$2 >>>"* ]]
}

# outside_lines FILE NAME: the lines of FILE outside the marked block of NAME.
outside_lines() {
  local file=$1 begin="# >>> devbox:$2 >>>" end="# <<< devbox:$2 <<<" inside=false line t
  [ -f "$file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    t=$(trim "$line")
    if ! $inside && [ "$t" = "$begin" ]; then
      inside=true
    elif $inside && [ "$t" = "$end" ]; then
      inside=false
    elif ! $inside; then
      printf '%s\n' "$line"
    fi
  done <"$file"
}

# outside_matches FILE NAME REGEX: whether a line of FILE outside the block of NAME
# matches the extended regular expression REGEX.
outside_matches() {
  local line
  while IFS= read -r line; do
    [[ ! $line =~ $3 ]] || return 0
  done < <(outside_lines "$1" "$2")
  return 1
}

CREATED_BLOCK="pip-created"

# mark_created FILE [MODE]: when FILE does not exist yet, records in a marked block of
# its own that DevBox creates it. The body is a comment, valid in every file the
# plugin writes (INI, TOML, netrc, shell). Call it before the first block is written.
mark_created() {
  local file=$1 mode=${2:-644}
  [ ! -e "$file" ] || return 0
  printf '# File created by DevBox.\n' | sdk block "$file" "$CREATED_BLOCK" --mode "$mode"
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

# insert_point FILE: the line before which a new block goes so that it directly follows
# a non-blank line or starts the file (parsers such as the netrc module of Python stop
# at a comment after a blank line). Prints nothing when the block goes at the end.
insert_point() {
  local file=$1 prev="" first=true line t
  local -A seen=()
  [ -f "$file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    t=$(trim "$line")
    if [ -n "$t" ] && { $first || [ -n "$prev" ]; } && [ -z "${seen[$t]:-}" ]; then
      printf '%s' "$t"
      return 0
    fi
    [ -z "$t" ] || seen[$t]=1
    prev=$t
    first=false
  done <"$file"
}

# global_insert_point FILE NAME: for a pip.conf whose own [global] section lies outside
# the block of NAME, the section header after it, before which a new block goes (empty:
# at the end of the file).
global_insert_point() {
  local in_global=false line t
  while IFS= read -r line; do
    t=$(trim "$line")
    if [[ $t =~ ^\[.*\]$ ]]; then
      if $in_global; then
        printf '%s' "$t"
        return 0
      fi
      [ "$t" != "[global]" ] || in_global=true
    fi
  done < <(outside_lines "$1" "$2")
}

# put_block FILE NAME MODE [BEFORE]: writes stdin as the block of NAME; a new block goes
# before the line BEFORE when it is given.
put_block() {
  local file=$1 name=$2 mode=$3 before=${4:-}
  if [ -n "$before" ] && ! has_block "$file" "$name"; then
    sdk block "$file" "$name" --mode "$mode" --before "$before"
  else
    sdk block "$file" "$name" --mode "$mode"
  fi
}
