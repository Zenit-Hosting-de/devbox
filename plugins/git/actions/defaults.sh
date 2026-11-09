#!/usr/bin/env bash
# Action: writes the values of the setting defaults (config key -> value) into a marked
# block of the Git configuration. The block is replaced as a whole, so keys taken out
# of the setting disappear again; an empty setting removes the block.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

block="git:defaults"
keys=()
while IFS= read -r k; do
  [ -z "$k" ] || keys+=("$k")
done < <(sdk json-keys "$DEVBOX_REQUEST" /settings/defaults 2>/dev/null || true)

if [ "${#keys[@]}" -eq 0 ]; then
  sdk emit step defaults running "no defaults"
  drop_block "$GITCONFIG" "$block"
  sdk emit step defaults "done" "no defaults"
  sdk emit result "No Git defaults set"
  exit 0
fi

text=""
section=""
for k in "${keys[@]}"; do
  value=$(sdk json-get "$DEVBOX_REQUEST" "/settings/defaults/$(json_pointer_token "$k")")
  entry=$(gc_entry "$k" "$value" "$section") || fail "not a Git configuration key: $k"
  text+="$entry"$'\n'
  section=$(gc_section_line "$k")
done

sdk emit step defaults running "${#keys[@]} values"
mark_created "$GITCONFIG"
printf '%s' "$text" | sdk block "$GITCONFIG" "$block"
sdk emit step defaults "done" "${#keys[@]} values"
sdk emit result "${#keys[@]} Git defaults set"
