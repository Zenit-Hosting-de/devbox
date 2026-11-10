#!/usr/bin/env bash
# Check: is Docker installed, in which version, is the user in the group and does the
# daemon answer?
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

source=$(sdk setting source)
group=$(sdk setting group)
user=$(group_user)

if ! command -v docker >/dev/null 2>&1; then
  printf '{"status":"missing","summary":"Docker is not installed","data":{"installed":false,"version":null,"serverVersion":null,"reachable":false,"group":%s,"inGroup":false,"source":%s},"hints":[{"text":"Install Docker Engine","action":"docker.install"}]}\n' \
    "$(json_str "$group")" "$(json_str "$source")"
  exit 0
fi

# "Docker version 27.5.1, build 9f9e405" -> 27.5.1
version=$(docker --version 2>/dev/null) || version=""
version=${version#Docker version }
version=${version%%,*}
[ -n "$version" ] || version=unknown

member=false
! in_group "$user" "$group" || member=true

reachable=false
server=$(docker version --format '{{.Server.Version}}' 2>/dev/null) || server=""
[ -z "$server" ] || reachable=true

status=ok
summary="Docker $version"
hints=""
if ! $reachable; then
  status=warn
  if $member; then
    summary="Docker $version, the daemon does not answer"
    hints='{"text":"Start the Docker service, or log in again when the group was just added"}'
  else
    summary="Docker $version, the daemon does not answer and $user is not in the group $group"
    hints="{\"text\":$(json_str "Add $user to the group $group"),\"action\":\"docker.group\"}"
  fi
fi
server_json=null
[ -z "$server" ] || server_json=$(json_str "$server")
member_text=no
! $member || member_text=yes
reach_text="no answer"
! $reachable || reach_text="running, version $server"

printf '{"status":"%s","summary":%s,"data":{"installed":true,"version":%s,"serverVersion":%s,"reachable":%s,"group":%s,"inGroup":%s,"source":%s},"details":[{"label":"Client","value":%s},{"label":"Daemon","value":%s},{"label":%s,"value":%s}],"hints":[%s]}\n' \
  "$status" "$(json_str "$summary")" "$(json_str "$version")" "$server_json" "$reachable" \
  "$(json_str "$group")" "$member" "$(json_str "$source")" \
  "$(json_str "$version")" "$(json_str "$reach_text")" \
  "$(json_str "$user in the group $group")" "$(json_str "$member_text")" "$hints"
