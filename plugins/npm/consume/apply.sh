#!/usr/bin/env bash
# Consumer: writes a registry credential into the npm configuration.
#   mode file: marked block in .npmrc with registry lines and the token (mode 0600)
#   mode env:  marked block in .npmrc that refers to a variable, plus a marked block in
#              the shell environment file (mode 0600) that exports it
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
base=$(sdk field url)
layout=$(sdk field layout)
user=$(sdk field username 2>/dev/null) || user=""
repo=$(sdk option repository 2>/dev/null) || repo=""
url=$(sdk option url 2>/dev/null) || url=""
scopes=()
while IFS= read -r s; do
  [ -z "$s" ] || scopes+=("$s")
done < <(sdk json-get "$DEVBOX_REQUEST" '/credential/options/scopes/*' 2>/dev/null || true)

registry=$(registry_url "$layout" "$base" "$repo" "$url") || exit 2
key=$(auth_key "$registry")
block="npm:$name"
var=$(var_name "$name")
secret=$(sdk secret)

# nexus and artifactory with a user name use basic authentication (_auth), everything
# else sends the secret as a token.
auth=_authToken
basic=false
case "$layout" in
nexus | artifactory) [ -z "$user" ] || basic=true ;;
esac
if $basic; then
  auth=_auth
  value=$(printf '%s:%s' "$user" "$secret" | base64 -w0)
else
  value=$secret
fi

# The text of the block is built in shell variables and piped with the printf builtin,
# so the secret never appears in the arguments of a process or in a temporary file.
text=""
if [ "${#scopes[@]}" -eq 0 ]; then
  text+="registry=$registry"$'\n'
else
  for s in "${scopes[@]}"; do text+="$s:registry=$registry"$'\n'; done
fi
if [ "$MODE" = env ]; then
  # shellcheck disable=SC2016 # the variable is expanded by npm, not here
  text+="$key:$auth=\${$var}"$'\n'
else
  text+="$key:$auth=$value"$'\n'
fi
if [ "$ALWAYS_AUTH" = true ]; then
  text+="$key:always-auth=true"$'\n'
fi

sdk emit step "apply-$name" running "$registry"
if [ "$MODE" = env ]; then
  is_dry_run || { [ ! -e "$ENV_FILE" ] || chmod 600 "$ENV_FILE"; }
  mark_created "$ENV_FILE" 600
  printf 'export %s=%s\n' "$var" "$(shell_quote "$value")" | sdk block "$ENV_FILE" "$block" --mode 600
  mark_created "$NPMRC"
  printf '%s' "$text" | sdk block "$NPMRC" "$block"
else
  is_dry_run || { [ ! -e "$NPMRC" ] || chmod 600 "$NPMRC"; }
  mark_created "$NPMRC" 600
  printf '%s' "$text" | sdk block "$NPMRC" "$block" --mode 600
  drop_block "$ENV_FILE" "$block"
fi
sdk emit step "apply-$name" "done" "$registry"
