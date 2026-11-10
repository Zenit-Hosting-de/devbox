#!/usr/bin/env bash
# Action: create a project folder from a template. Copies the files the template
# selects and replaces {{key}} placeholders in the files it marks for rendering. The
# commands under hooks.postCreate are only listed; the action hooks runs them.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

# glob_re GLOB: the glob of a template as an anchored regular expression. * and ? stay
# within one path segment, ** spans segments; every other character stands for itself.
glob_re() {
  local glob=$1 re='^' c i=0
  while [ "$i" -lt "${#glob}" ]; do
    c=${glob:i:1}
    if [ "${glob:i:3}" = '**/' ]; then
      re+='(.*/)?'
      i=$((i + 3))
      continue
    elif [ "${glob:i:2}" = '**' ]; then
      re+='.*'
      i=$((i + 2))
      continue
    fi
    case "$c" in
    '*') re+='[^/]*' ;;
    '?') re+='[^/]' ;;
    '.' | '+' | '(' | ')' | '[' | ']' | '{' | '}' | '^' | '$' | '|' | \\) re+="\\$c" ;;
    *) re+=$c ;;
    esac
    i=$((i + 1))
  done
  printf '%s$' "$re"
}

# globs POINTER: the globs at a jq path of the template as regular expressions, one per line.
globs() {
  local glob
  while IFS= read -r glob; do
    glob_re "$glob"
    echo
  done < <(jq -r "$1 // [] | .[]" "$tpl")
}

# matches PATH RE…: whether a path matches one of the expressions.
matches() {
  local path=$1 re
  shift
  for re in "$@"; do
    [[ $path =~ $re ]] && return 0
  done
  return 1
}

id=$(sdk param template)
name=$(sdk param name)
given=$(sdk param values)

valid_name "$name" || fail 2 "name: $name is not usable as a folder name"
jq -e 'type == "object"' <<<"$given" >/dev/null 2>&1 || fail 2 "values: not a JSON object"
tdir=$(find_template "$id")
[ -n "$tdir" ] || fail 2 "template: no usable template $id in the fetched sources"
tpl="$tdir/template.json"
target=$(project_dir "$name")

if [ -e "$target" ] || [ -L "$target" ]; then
  [ -d "$target" ] && [ ! -L "$target" ] || fail 2 "name: $target exists and is not a folder"
  shopt -s nullglob dotglob
  entries=("$target"/*)
  shopt -u nullglob dotglob
  [ "${#entries[@]}" -eq 0 ] || fail 2 "name: $target exists and is not empty"
fi

# The values of the template parameters: given values, else defaults. A default that
# refers to a setting (${…}) cannot be resolved by a plugin and needs a given value.
resolved=$(jq -c --argjson given "$given" '
def oktype($t):
  if $t == "string" then type == "string"
  elif $t == "integer" then type == "number" and . == floor
  elif $t == "number" then type == "number"
  elif $t == "boolean" then type == "boolean"
  elif $t == "array" then type == "array"
  else type == "object" end;
def problem($f):
  if (oktype($f.type) | not) then "is not of type " + $f.type
  elif ($f | has("enum")) and (. as $v | any($f.enum[]; . == $v) | not) then "is not one of " + ($f.enum | tojson)
  elif type == "string" and ($f | has("pattern")) and (. as $v | try ($v | test($f.pattern)) catch true | not) then "does not match " + $f.pattern
  elif type == "string" and ($f | has("minLength")) and length < $f.minLength then "is shorter than \($f.minLength) characters"
  elif type == "string" and ($f | has("maxLength")) and length > $f.maxLength then "is longer than \($f.maxLength) characters"
  elif type == "number" and ($f | has("minimum")) and . < $f.minimum then "is less than \($f.minimum)"
  elif type == "number" and ($f | has("maximum")) and . > $f.maximum then "is greater than \($f.maximum)"
  else null end;
(.params // {}) as $params
| reduce ($params | to_entries[]) as $e (
    {errors: [$given | keys[] | select(. as $k | $params | has($k) | not) | "values: " + . + " is not a parameter of the template"], values: {}};
    $e.key as $k | $e.value as $f
    | (if ($given | has($k)) then [$given[$k]]
       elif ($f | has("default")) and ($f.default | (type != "string") or (test("\\$\\{") | not)) then [$f.default]
       else [] end) as $v
    | if ($v | length) == 0 then .errors += ["values: " + $k + " needs a value"]
      else ($v[0] | problem($f)) as $p
        | if $p then .errors += ["values: " + $k + " " + $p]
          else .values[$k] = ($v[0] | if type == "string" then . else tojson end) end
      end)' "$tpl")
mapfile -t errors < <(jq -r '.errors[]' <<<"$resolved")
if [ "${#errors[@]}" -gt 0 ]; then
  for e in "${errors[@]}"; do
    sdk emit log error "${e:0:400}"
  done
  exit 2
fi
values=$(jq -c '.values' <<<"$resolved")

missing=""
while IFS= read -r c; do
  command -v "$c" >/dev/null 2>&1 || missing+="${missing:+, }$c"
done < <(jq -r '.requires.commands // [] | .[]' "$tpl")
[ -z "$missing" ] || fail 3 "the template needs commands that are not installed: ${missing:0:300}"
plugins=$(jq -r '.requires.plugins // {} | to_entries | map(.key + " " + .value) | join(", ")' "$tpl")
if [ -n "$plugins" ]; then
  sdk emit log info "The template expects these plugins, which is not checked: ${plugins:0:300}"
fi

mapfile -t include < <(globs .files.include)
mapfile -t exclude < <(globs .files.exclude)
mapfile -t render < <(globs .files.render)

sdk emit step create running "$name from $id"
if [ ! -d "$target" ]; then
  is_dry_run || mkdir -p "$target"
  sdk emit change file create "$target" "project folder"
fi
count=0
while IFS= read -r -d '' src; do
  rel=${src#"$tdir"/}
  [ "$rel" != template.json ] || continue
  [ "${#include[@]}" -eq 0 ] || matches "$rel" "${include[@]}" || continue
  ! matches "$rel" "${exclude[@]}" || continue
  dest="$target/$rel"
  how=copied
  if matches "$rel" "${render[@]}"; then
    how=rendered
  fi
  if ! is_dry_run; then
    mkdir -p "${dest%/*}"
    if [ "$how" = rendered ]; then
      jq -Rsj --argjson values "$values" \
        'gsub("\\{\\{(?<key>[a-z][A-Za-z0-9]{0,47})\\}\\}"; .key as $k | $values[$k] // ("{{" + $k + "}}"))' "$src" >"$dest"
      if [ -x "$src" ]; then
        chmod +x "$dest"
      fi
    else
      cp -- "$src" "$dest"
    fi
  fi
  sdk emit change file create "$dest" "$how from template $id"
  count=$((count + 1))
done < <(find "$tdir" -type f -print0 | LC_ALL=C sort -z)
sdk emit step create "done" "$name from $id"

links=0
while IFS= read -r -d '' _; do
  links=$((links + 1))
done < <(find "$tdir" -type l -print0)
if [ "$links" -gt 0 ]; then
  sdk emit log warn "$links symbolic links of the template are not copied"
fi

hooks=0
while IFS= read -r line; do
  sdk emit log info "Command of the template, not run here: ${line:0:400}"
  hooks=$((hooks + 1))
done < <(jq -r '.hooks.postCreate // [] | .[] | map(@sh) | join(" ")' "$tpl")
if [ "$hooks" -gt 0 ]; then
  sdk emit log info "The action templates.hooks runs these commands in the project folder after a confirmation."
fi

sdk emit result "$name from $id, $count files" \
  "$(jq -cn --arg path "$target" --arg template "$id" --argjson files "$count" --argjson hooks "$hooks" \
    '{path: $path, template: $template, files: $files, hooks: $hooks}')"
