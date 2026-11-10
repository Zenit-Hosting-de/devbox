#!/usr/bin/env bash
# Action: write the configured settings into daemon.json and keep every other key.
# The file is edited as a working copy with devbox sdk json-set and installed (as root
# where needed) only when it differs. The plugin records which keys it wrote and what
# was there before, so that emptying a setting puts the old state back.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

file=$(sdk setting daemonConfig)
service=$(sdk setting service)
reload=$(sdk setting serviceReload)
[ -n "$file" ] || fail "The setting daemonConfig is empty" 2
record="$DEVBOX_STATE_DIR/daemon.json"

stage_dir
work="$STAGE/daemon.json"
state="$STAGE/state.json"
existed=false
if [ -e "$file" ]; then
  [ -r "$file" ] || fail "Cannot read $file"
  cp "$file" "$work"
  sdk json-keys "$work" "" >/dev/null 2>&1 || fail "$file is not a JSON object; nothing changed"
  existed=true
fi
if [ -f "$record" ]; then cp "$record" "$state"; else printf '{}\n' >"$state"; fi

managed=0
for entry in "${DAEMON_KEYS[@]}"; do
  read -r setting key type <<<"$entry"
  want=$(daemon_wanted "$setting" "$type")
  recorded=false
  ! sdk json-keys "$state" "/keys/$key" >/dev/null 2>&1 || recorded=true
  if [ -n "$want" ]; then
    managed=$((managed + 1))
    if have=$(daemon_current "$work" "$key" "$type"); then
      [ "$have" != "$want" ] || continue
      $recorded || stage_sdk json-set "$state" "/keys/$key/previous" "$have"
    else
      $recorded || stage_sdk json-set "$state" "/keys/$key" '{}'
    fi
    stage_sdk json-set "$work" "/$key" "$want"
  elif $recorded; then
    # The setting is empty again: back to what was there before DevBox wrote the key.
    if previous=$(daemon_current "$state" "keys/$key/previous" "$type"); then
      stage_sdk json-set "$work" "/$key" "$previous"
    else
      stage_sdk json-remove "$work" "/$key"
    fi
    stage_sdk json-remove "$state" "/keys/$key"
  fi
done

changed=false
if [ "$managed" -eq 0 ] && [ "$(sdk json-get "$state" /created 2>/dev/null)" = true ] &&
  [ -f "$work" ] && ! has_keys "$work" ""; then
  # DevBox created the file and nothing is left in it.
  if $existed; then
    drop_file "$file" "created by devbox, empty again"
    changed=true
  fi
  printf '{}\n' >"$state"
elif [ -f "$work" ]; then
  $existed || stage_sdk json-set "$state" /created true
  if differs "$work" "$file"; then
    put_file "$work" "$file" 644 "daemon settings written"
    changed=true
  fi
fi
[ "$managed" -gt 0 ] || has_keys "$state" /keys || printf '{}\n' >"$state"
is_dry_run || install -D -m 600 "$state" "$record"

if ! $changed; then
  sdk emit result "daemon.json is up to date"
  exit 0
fi

if [ "$reload" != none ] && service_active "$service"; then
  sdk emit change service update "$service" "$reload after the change of daemon.json"
  is_dry_run || as_root systemctl "$reload" "$service"
elif ! is_dry_run; then
  sdk emit log info "The change takes effect when the service $service is restarted"
fi
if is_dry_run; then
  sdk emit result "daemon.json would be written"
else
  sdk emit result "daemon.json written"
fi
