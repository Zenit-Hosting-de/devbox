#!/usr/bin/env bash
# Check: secrets of credential instances that expired or expire within warnDays days.
# Reads only the credentials directory (metadata, no secrets).
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

file=$(sdk setting file)
warn_days=$(sdk setting warnDays)
now=$(date -u +%s)

status=ok
items=""
expired=""
soon=""
dated=0
first=""
if [ -f "$file" ]; then
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    at=$(sdk json-get "$file" "/instances/$name/secret/expiresAt" 2>/dev/null) || continue
    [ "$at" != null ] || continue
    ts=$(date -u -d "$at" +%s) || continue
    dated=$((dated + 1))
    left=$((ts - now))
    days=$(((left + 86399) / 86400))
    if [ "$left" -le 0 ]; then
      status=error
      expired+="${expired:+, }$name expired on ${at%%T*}"
      first=${first:-$name}
    elif [ "$left" -le $((warn_days * 86400)) ]; then
      [ "$status" = error ] || status=warn
      soon+="${soon:+, }$name expires in $days days"
      first=${first:-$name}
    else
      continue
    fi
    items+="${items:+,}{\"instance\":$(json_str "$name"),\"expiresAt\":$(json_str "$at"),\"days\":$((left > 0 ? days : 0))}"
  done < <(sdk json-keys "$file" /instances)
fi

summary="$expired${expired:+${soon:+, }}$soon"
if [ -z "$summary" ]; then
  if [ "$dated" -eq 0 ]; then
    summary="No credential with an expiry date"
  else
    summary="No credential expires within $warn_days days"
  fi
fi
hints=""
if [ -n "$first" ]; then
  hints=",\"hints\":[{\"text\":$(json_str "Set a new secret: devbox cred set $first")}]"
fi
printf '{"status":"%s","summary":%s,"data":{"items":[%s]}%s}\n' "$status" "$(json_str "$summary")" "$items" "$hints"
