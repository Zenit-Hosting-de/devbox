#!/usr/bin/env bash
# Action: verify the registry credential of the request against the registry. The
# request comes from `devbox cred set|verify`; run without a credential, the action
# has nothing to verify and succeeds. It reads only, so a dry run behaves the same.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if ! instance=$(sdk instance 2>/dev/null); then
  sdk emit step verify skipped "No credential to verify"
  sdk emit result "No credential to verify"
  exit 0
fi

url=$(sdk field url)
layout=$(sdk field layout 2>/dev/null) || layout=custom
user=$(sdk field username 2>/dev/null) || user=""
path=$(sdk json-get "$DEVBOX_REQUEST" "/settings/verifyPaths/$layout" 2>/dev/null) || path=""
timeout=$(sdk setting timeout)
secret=$(sdk secret)
case "$secret$user$url" in
*$'\n'* | *$'\r'*)
  echo "The secret, user name or address contains a line break." >&2
  exit 2
  ;;
esac

target="${url%/}$path"
label="Verify $instance at $target"
sdk emit step verify running "$label"
if ! code=$(curl_config "$target" "$layout" "$user" "$secret" |
  curl -K - --silent --show-error --output /dev/null --write-out '%{http_code}' --max-time "$timeout"); then
  sdk emit step verify failed "$label"
  sdk emit result "Registry not reachable: $target"
  exit 3
fi

if [ -z "$path" ]; then
  sdk emit step verify "done" "$label"
  sdk emit result "Registry reachable (HTTP $code); the credential itself is not checked"
  exit 0
fi
case "$code" in
2??)
  sdk emit step verify "done" "$label"
  sdk emit result "Accepted by the registry (HTTP $code)"
  ;;
401 | 403)
  sdk emit step verify failed "$label"
  sdk emit result "Rejected by the registry (HTTP $code)"
  exit 1
  ;;
*)
  sdk emit step verify failed "$label"
  sdk emit result "Unexpected answer of the registry (HTTP $code)"
  exit 1
  ;;
esac
