#!/usr/bin/env bash
# Consumer: writes a registry credential as login into the Docker client configuration
# (auths.<host>.auth, base64 of user:secret, file mode 0600), like docker login does.
# The value is built in shell variables and handed to devbox sdk on standard input, so
# the secret never appears in the arguments of a process or in a temporary file.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
url=$(sdk field url)
user=$(sdk field username 2>/dev/null) || user=""
host=$(sdk option host 2>/dev/null) || host=""
file=$(sdk setting clientConfig)
[ -n "$host" ] || host=$(url_host "$url")
[ -n "$user" ] || user=$(sdk setting tokenUsername)

[ -n "$file" ] || fail "The setting clientConfig is empty" 2
valid_host "$host" || fail "The registry host is empty or has a blank, slash, quote or backslash" 2
[ -n "$user" ] || fail "The credential has no user name and the setting tokenUsername is empty" 2
[[ $user != *:* ]] || fail "The user name must not contain a colon" 2

entry="/auths/$(pointer_token "$host")"
if [ -e "$file" ]; then
  [ -r "$file" ] || fail "Cannot read $file"
  sdk json-keys "$file" "" >/dev/null 2>&1 || fail "$file is not a JSON object; nothing changed"
fi

# A login this instance wrote for another host goes first.
previous=$(state_get "/instances/$name/host")
if [ -n "$previous" ] && [ "$previous" != "$host" ]; then
  remove_login "$name" "$previous" "$file"
  previous=""
fi
# A login for the host that DevBox did not write is not overwritten: removing could
# not bring it back.
if [ -z "$previous" ] && [ -f "$file" ] && sdk json-get "$file" "$entry" >/dev/null 2>&1; then
  fail "$file already has a login for $host that DevBox did not write; remove it first (docker logout $host)"
fi

sdk emit step "apply-$name" running "$host"
if [ -z "$previous" ]; then
  # Record what is created, before it is created, so that removing can undo it.
  dir=${file%/*}
  [ -d "$dir" ] || [ -n "$(state_get /created/dir)" ] || state_set /created/dir "$(json_str "$dir")"
  [ -e "$file" ] || state_set /created/file true
  { [ -f "$file" ] && sdk json-get "$file" /auths >/dev/null 2>&1; } || state_set /created/auths true
  state_set "/instances/$name/host" "$(json_str "$host")"
fi
if [ -f "$file" ]; then
  store=$(sdk json-get "$file" /credsStore 2>/dev/null) || store=""
  [ -z "$store" ] || sdk emit log warn "$file names the credential store $store; Docker may prefer it over the login in the file"
  is_dry_run || chmod 600 "$file"
fi

secret=$(sdk secret)
auth=$(printf '%s:%s' "$user" "$secret" | base64 -w0)
printf '"%s"' "$auth" | sdk json-set --mode 600 "$file" "$entry/auth" -
sdk emit step "apply-$name" "done" "$host"
sdk emit result "Login for $host written"
