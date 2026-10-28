#!/usr/bin/env bash
# Consumer: write a registry credential into settings.xml (server, optional mirror,
# repository profile) as marked blocks. Idempotent.
set -euo pipefail
# shellcheck source=../lib/consume.sh
. "$DEVBOX_PLUGIN_DIR/lib/consume.sh"

name=$(sdk instance)
check_name "$name"
id=$(sdk option id 2>/dev/null) || id=""
id=${id:-$name}
mirror_of=$(sdk option mirrorOf 2>/dev/null) || mirror_of=""
snapshots=$(sdk option snapshots 2>/dev/null) || snapshots=false
[ "$snapshots" = true ] || snapshots=false
user=$(sdk field username 2>/dev/null) || user=""
[ -n "$user" ] || fail "the credential $name has no field username, which Maven needs for the server $id"
url=$(repo_url)
token=$(sdk secret)

ensure_file

e_id=$(xml_escape "$id")
e_url=$(xml_escape "$url")

{
  printf '    <server>\n      <id>%s</id>\n      <username>%s</username>\n      <password>%s</password>\n    </server>\n' \
    "$e_id" "$(xml_escape "$user")" "$(xml_escape "$token")"
} | put_block servers "maven:$name"

if [ -n "$mirror_of" ]; then
  printf '    <mirror>\n      <id>%s</id>\n      <mirrorOf>%s</mirrorOf>\n      <url>%s</url>\n    </mirror>\n' \
    "$e_id" "$(xml_escape "$mirror_of")" "$e_url" | put_block mirrors "maven:$name:mirror"
else
  sdk block-remove "$SETTINGS" "maven:$name:mirror" --comment xml
fi

printf '    <profile>\n      <id>devbox-%s</id>\n      <repositories>\n        <repository>\n          <id>%s</id>\n          <url>%s</url>\n          <releases>\n            <enabled>true</enabled>\n          </releases>\n          <snapshots>\n            <enabled>%s</enabled>\n          </snapshots>\n        </repository>\n      </repositories>\n    </profile>\n' \
  "$(xml_escape "$name")" "$e_id" "$e_url" "$snapshots" | put_block profiles "maven:$name:profile"
printf '    <activeProfile>devbox-%s</activeProfile>\n' "$(xml_escape "$name")" | put_block activeProfiles "maven:$name:active"

secure_file
sdk emit result "Maven server $id written to $SETTINGS"
