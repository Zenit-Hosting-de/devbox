#!/usr/bin/env bash
# Action: remove an installed .NET SDK.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
version=$(installed_match "$spec") || {
  sdk emit result ".NET SDK $spec is not installed"
  exit 0
}
remove_sdk "$version"
sdk emit result "Removed .NET SDK $version"
