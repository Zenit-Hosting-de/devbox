#!/usr/bin/env bash
# Consumer: writes a registry credential as Gradle project properties
# <base>Username, <base>Password and <base>Url (only with the option repository or url).
#   mode file: marked block in gradle.properties (mode 0600)
#   mode env:  marked block in the shell environment file (mode 0600) that exports the
#              same names as ORG_GRADLE_PROJECT_<property>
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
base_url=$(sdk field url)
layout=$(sdk field layout 2>/dev/null) || layout=nexus
user=$(sdk field username 2>/dev/null) || user=""
repo=$(sdk option repository 2>/dev/null) || repo=""
url=$(sdk option url 2>/dev/null) || url=""
gname=$(sdk option name 2>/dev/null) || gname=""

[[ $PREFIX =~ ^([A-Za-z_][A-Za-z0-9_]*)?$ ]] || fail "setting propertyPrefix $PREFIX is not a valid property name start"
[ -n "$gname" ] || gname=$(camel "$name")
[[ $gname =~ ^[A-Za-z][A-Za-z0-9]*$ ]] || fail "option name $gname is not a valid Gradle repository name"
prop=$(prop_base "$PREFIX" "$gname")
repository=$(repo_url "$layout" "$base_url" "$repo" "$url") || fail "layout $layout needs the option url with the full repository address"
[ -n "$user" ] || sdk emit log warn "the credential $name has no field username; only ${prop}Password is written"
block="gradle:$name"
secret=$(sdk secret)

# The text of the block is built in shell variables and piped with the printf builtin,
# so the secret never appears in the arguments of a process or in a temporary file.
text=""
if [ "$MODE" = env ]; then
  [ -z "$user" ] || text+="export ORG_GRADLE_PROJECT_${prop}Username=$(shell_quote "$user")"$'\n'
  text+="export ORG_GRADLE_PROJECT_${prop}Password=$(shell_quote "$secret")"$'\n'
  [ -z "$repository" ] || text+="export ORG_GRADLE_PROJECT_${prop}Url=$(shell_quote "$repository")"$'\n'
else
  [ -z "$user" ] || text+="${prop}Username=$(prop_escape "$user")"$'\n'
  text+="${prop}Password=$(prop_escape "$secret")"$'\n'
  [ -z "$repository" ] || text+="${prop}Url=$(prop_escape "$repository")"$'\n'
fi

sdk emit step "apply-$name" running "$prop"
if [ "$MODE" = env ]; then
  secure "$ENV_FILE"
  mark_created "$ENV_FILE"
  printf '%s' "$text" | sdk block "$ENV_FILE" "$block" --mode 600
  drop_block "$PROPS" "$block"
else
  secure "$PROPS"
  mark_created "$PROPS"
  printf '%s' "$text" | sdk block "$PROPS" "$block" --mode 600
  drop_block "$ENV_FILE" "$block"
fi
sdk emit step "apply-$name" "done" "$prop"
sdk emit result "Gradle properties ${prop}* written ($MODE)"
