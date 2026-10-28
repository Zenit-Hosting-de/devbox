#!/usr/bin/env bash
# Shared functions of the credentials plugin. Sourced by checks.
# Uses only devbox sdk and the commands listed in requires.commands.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}
