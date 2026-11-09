#!/usr/bin/env bash
# Action: install uv, the requested Python version and the configured versions and
# tools, and put the commands in PATH of new shells.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

spec=$(sdk param version)
make_default=$(sdk param makeDefault)
if [ -n "$spec" ] && ! valid_spec "$spec"; then
  sdk emit log error "invalid version: $spec"
  exit 2
fi
specs=()
while IFS= read -r v; do
  [ -n "$v" ] || continue
  if ! valid_spec "$v"; then
    sdk emit log error "invalid version in python.versions: $v"
    exit 2
  fi
  specs+=("$v")
done < <(setting_list versions)

install_uv
write_env

spec=$(resolve_spec "$spec")
install_python "$spec"
for s in "${specs[@]}"; do
  [ "$s" = "$spec" ] || install_python "$s"
done
if [ "$make_default" = true ]; then
  set_default "$spec"
fi
install_tools

version=$(installed_match "$spec" || true)
sdk emit result "Python ${version:-${spec:-newest release}}" "{\"version\":$(json_str "${version:-$spec}")}"
