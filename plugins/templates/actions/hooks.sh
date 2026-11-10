#!/usr/bin/env bash
# Action: run the commands a template lists under hooks.postCreate in a project folder.
# The manifest asks for a confirmation before this action runs; the dry run lists every
# command. The commands run once per project unless the parameter again is set.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

id=$(sdk param template)
name=$(sdk param name)
again=$(sdk param again)

valid_name "$name" || fail 2 "name: $name is not usable as a folder name"
tdir=$(find_template "$id")
[ -n "$tdir" ] || fail 2 "template: no usable template $id in the fetched sources"
tpl="$tdir/template.json"
target=$(project_dir "$name")
[ -d "$target" ] || fail 2 "name: there is no project folder $target"

mapfile -t hooks < <(jq -c '.hooks.postCreate // [] | .[]' "$tpl")
if [ "${#hooks[@]}" -eq 0 ]; then
  sdk emit result "Template $id lists no commands" '{"commands":0}'
  exit 0
fi

marker="$DEVBOX_STATE_DIR/hooks/$(short_hash "$target").json"
if [ -f "$marker" ] && [ "$again" != true ]; then
  sdk emit step hooks skipped "Commands of $id ran for $name before"
  sdk emit result "Commands of $id ran for $name before" '{"commands":0}'
  exit 0
fi

# Every program must be there before the first command runs.
cd "$target"
for hook in "${hooks[@]}"; do
  program=$(jq -r '.[0]' <<<"$hook")
  command -v "$program" >/dev/null 2>&1 || fail 3 "the template needs a command that is not installed: ${program:0:300}"
done

n=0
for hook in "${hooks[@]}"; do
  n=$((n + 1))
  label=$(jq -r 'map(@sh) | join(" ")' <<<"$hook")
  label=${label:0:400}
  if ! is_dry_run; then
    mapfile -t cmd < <(jq -r '.[]' <<<"$hook")
    sdk emit step "hook-$n" running "$label"
    printf '$ %s\n' "$label"
    if ! "${cmd[@]}"; then
      sdk emit step "hook-$n" failed "$label"
      fail 1 "command $n of template $id failed"
    fi
    sdk emit step "hook-$n" "done" "$label"
  fi
  sdk emit change other update "$target" "run: $label"
done

if ! is_dry_run; then
  mkdir -p "${marker%/*}"
  jq -n --arg template "$id" --arg path "$target" '{template: $template, path: $path}' >"$marker"
fi
sdk emit result "$n commands of $id in $name" "{\"commands\":$n}"
