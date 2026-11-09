#!/usr/bin/env bash
# Check: which registry credentials DevBox keeps for Gradle, and where.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

# blocks FILE: the instance names of the gradle:NAME blocks in FILE, one per line.
blocks() {
  local line re='^# >>> devbox:gradle:([a-z0-9-]+) >>>$'
  [ -f "$1" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    [[ $line =~ $re ]] && printf '%s\n' "${BASH_REMATCH[1]}"
  done <"$1"
  return 0
}

for f in "$PROPS" "$ENV_FILE"; do
  if [ -e "$f" ] && [ ! -r "$f" ]; then
    printf '{"status":"error","summary":%s,"data":{"mode":%s,"credentials":[]}}\n' \
      "$(json_str "cannot read $f")" "$(json_str "$MODE")"
    exit 0
  fi
done

items=""
names=""
misplaced=""
for place in file env; do
  f=$PROPS
  [ "$place" = file ] || f=$ENV_FILE
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    items+="${items:+,}{\"instance\":$(json_str "$n"),\"stored\":\"$place\"}"
    names+="${names:+, }$n"
    [ "$place" = "$MODE" ] || misplaced+="${misplaced:+, }$n"
  done < <(blocks "$f")
done

details="[{\"label\":\"gradle.properties\",\"value\":$(json_str "$PROPS")},{\"label\":\"Environment file\",\"value\":$(json_str "$ENV_FILE")}]"
data="{\"mode\":$(json_str "$MODE"),\"credentials\":[$items]}"
if [ -z "$names" ]; then
  printf '{"status":"ok","summary":"No registry credentials","data":%s,"details":%s}\n' "$data" "$details"
elif [ -n "$misplaced" ]; then
  printf '{"status":"warn","summary":%s,"data":%s,"details":%s,"hints":[{"text":%s}]}\n' \
    "$(json_str "$names")" "$data" "$details" \
    "$(json_str "Stored outside mode $MODE: $misplaced. Applying the credential again moves it.")"
else
  printf '{"status":"ok","summary":%s,"data":%s,"details":%s}\n' "$(json_str "$names ($MODE)")" "$data" "$details"
fi
