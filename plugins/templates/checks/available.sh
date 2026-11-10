#!/usr/bin/env bash
# Check: the templates of all fetched sources. Reads only the local copies.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

total=${#SOURCES[@]}
fetched=0
for src in "${SOURCES[@]}"; do
  if [ -d "$(source_dir "$src")/.git" ]; then
    fetched=$((fetched + 1))
  fi
done

scan | jq -cs --argjson total "$total" --argjson fetched "$fetched" '
  (reduce (.[] | select(.errors == [])) as $t ({seen: {}, items: [], hidden: []};
    if .seen[$t.template.id] then .hidden += [$t] else .seen[$t.template.id] = true | .items += [$t] end)) as $r
  | [.[] | select(.errors != [])] as $bad
  | ($r.items | map(.source as $source | .template | {
      id, name,
      description: (.description // ""),
      language: (.language // "other"),
      tags: (.tags // []),
      params: (.params // {} | keys_unsorted),
      hooks: (.hooks.postCreate // [] | length),
      source: $source
    })) as $items
  | ($items | length) as $n
  | ($items | map(.language) | unique | join(", ")) as $languages
  | (if $total == 0 then {status: "ok", summary: "No template sources"}
     elif $fetched == 0 then {status: "missing", summary: "Templates not fetched"}
     elif $n == 0 then {status: "warn", summary: "No templates in the fetched sources"}
     else {
       status: (if ($bad | length) > 0 or ($r.hidden | length) > 0 or $fetched < $total then "warn" else "ok" end),
       summary: ((if $n == 1 then "1 template" else "\($n) templates" end) + " (" + $languages + ")")[0:500]
     } end)
  + {
      data: {items: $items},
      details: (
        [{label: "Sources", value: "\($fetched) of \($total) fetched"}]
        + ($bad | map({label: ("Invalid: " + (.dir | split("/") | last))[0:200], value: ((.errors | join("; ")) + " (" + .source + ")")}))
        + ($r.hidden | map({label: ("Hidden: " + .template.id), value: ("an earlier source has a template with this ID (" + .source + ")")}))
      ),
      hints: (
        (if $fetched < $total then [{text: "Fetch the templates", action: "templates.sync"}] else [] end)
        + (if $n > 0 then [{text: "Create a project", action: "templates.new"}] else [] end)
      )
    }'
