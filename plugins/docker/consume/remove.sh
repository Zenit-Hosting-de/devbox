#!/usr/bin/env bash
# Consumer: removes exactly the login that the instance wrote into the Docker client
# configuration. Logins that DevBox did not write stay.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
file=$(sdk setting clientConfig)
host=$(state_get "/instances/$name/host")
if [ -z "$host" ]; then
  sdk emit result "No login of $name to remove"
  exit 0
fi
sdk emit step "remove-$name" running "$host"
remove_login "$name" "$host" "$file"
sdk emit step "remove-$name" "done" "$host"
sdk emit result "Login for $host removed"
