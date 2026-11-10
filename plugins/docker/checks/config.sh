#!/usr/bin/env bash
# Check: does daemon.json hold the configured settings, and for which registries does
# the client configuration hold a login? Credentials are never shown, only the hosts.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

file=$(sdk setting daemonConfig)
client=$(sdk setting clientConfig)

# Registries with a login in the client configuration.
hosts=()
client_note=""
if [ -e "$client" ]; then
  if [ ! -r "$client" ]; then
    client_note="cannot read $client"
  elif ! sdk json-keys "$client" "" >/dev/null 2>&1; then
    client_note="$client is not a JSON object"
  else
    while IFS= read -r h; do
      [ -z "$h" ] || hosts+=("$h")
    done < <(sdk json-keys "$client" /auths 2>/dev/null || true)
  fi
fi
logins="none"
[ "${#hosts[@]}" -eq 0 ] || logins=$(IFS=,; echo "${hosts[*]}")
logins=${logins//,/, }
[ -z "$client_note" ] || logins=$client_note

# result STATUS SUMMARY DIFFERING_KEYS_JSON [HINT_JSON]
result() {
  printf '{"status":"%s","summary":%s,"data":{"daemonConfig":%s,"differs":%s,"registries":%s},"details":[{"label":"Daemon configuration","value":%s},{"label":"Registry logins","value":%s}],"hints":[%s]}\n' \
    "$1" "$(json_str "$2")" "$(json_str "$file")" "$3" "$(json_list "${hosts[@]}")" \
    "$(json_str "$file")" "$(json_str "$logins")" "${4:-}"
}

if [ -e "$file" ]; then
  if [ ! -r "$file" ]; then
    result warn "cannot read $file" '[]'
    exit 0
  fi
  if ! sdk json-keys "$file" "" >/dev/null 2>&1; then
    result error "$file is not a JSON object" '[]'
    exit 0
  fi
fi

wanted=0
differs=()
for entry in "${DAEMON_KEYS[@]}"; do
  read -r setting key type <<<"$entry"
  want=$(daemon_wanted "$setting" "$type")
  [ -n "$want" ] || continue
  wanted=$((wanted + 1))
  have=$(daemon_current "$file" "$key" "$type") || have=""
  [ "$have" = "$want" ] || differs+=("$key")
done

if [ "${#differs[@]}" -gt 0 ]; then
  list=$(IFS=,; echo "${differs[*]}")
  result warn "daemon.json differs from the settings: ${list//,/, }" "$(json_list "${differs[@]}")" \
    '{"text":"Write the settings into daemon.json","action":"docker.configure"}'
elif [ -n "$client_note" ]; then
  result warn "$client_note" '[]'
elif [ "$wanted" -eq 0 ]; then
  result ok "No daemon settings configured, registry logins: $logins" '[]'
else
  result ok "daemon.json holds the $wanted configured settings, registry logins: $logins" '[]'
fi
