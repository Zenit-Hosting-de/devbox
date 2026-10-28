#!/usr/bin/env bash
# Check: distribution, architecture and desktop of this machine (from the request).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

distro=$(host_field distro)
distro_version=$(host_field distroVersion)
arch=$(host_field arch)
desktop=$(host_field desktop)
name=$(host_field name)
user=$(host_field user)

system=${distro:-unknown}
[ -z "$distro_version" ] || system+=" $distro_version"
summary="$system, ${arch:-unknown}"
[ -z "$desktop" ] || summary+=", $desktop"

details="{\"label\":\"Distribution\",\"value\":$(json_str "$system")},{\"label\":\"Architecture\",\"value\":$(json_str "${arch:-unknown}")}"
details+=",{\"label\":\"Desktop\",\"value\":$(json_str "${desktop:-none}")}"
[ -z "$name" ] || details+=",{\"label\":\"Host name\",\"value\":$(json_str "$name")}"
[ -z "$user" ] || details+=",{\"label\":\"User\",\"value\":$(json_str "$user")}"

printf '{"status":"ok","summary":%s,"data":{"distro":%s,"distroVersion":%s,"arch":%s,"desktop":%s},"details":[%s]}\n' \
  "$(json_str "$summary")" "$(json_str "$distro")" "$(json_str "$distro_version")" "$(json_str "$arch")" "$(json_str "$desktop")" "$details"
