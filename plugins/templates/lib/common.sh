#!/usr/bin/env bash
# Shared functions of the templates plugin. Sourced by the check and the actions.
# Uses only devbox sdk and the commands listed in requires.commands.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

PROJECT_ROOT=$(sdk setting projectRoot)
CACHE_DIR=$(sdk setting cacheDir)
mapfile -t SOURCES < <(sdk setting sources | jq -r '.[]')
mapfile -t SEARCH_PATHS < <(sdk setting searchPaths | jq -r '.[]')

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# short_hash TEXT: the first 16 hex digits of the SHA-256 of TEXT.
short_hash() {
  local sum
  sum=$(printf '%s' "$1" | sha256sum)
  printf '%s' "${sum:0:16}"
}

# source_dir SOURCE: the folder that holds the copy of a source.
source_dir() { printf '%s/%s' "$CACHE_DIR" "$(short_hash "$1")"; }

# git_url SOURCE: the address Git uses for a source; fails for other schemes.
git_url() {
  case "$1" in
  git+https://?* | git+ssh://?* | git+file://?*) printf '%s' "${1#git+}" ;;
  *) return 2 ;;
  esac
}

# show SOURCE: a source for messages, without the user part of the address.
show() {
  local scheme=${1%%://*} rest=${1#*://} host
  host=${rest%%/*}
  printf '%s://%s%s' "$scheme" "${host##*@}" "${rest#"$host"}"
}

# scan: one JSON line per template folder of all fetched sources, in the order of the
# sources: {source, dir, errors, template}. errors is empty for a usable template; it
# lists what breaks the rules of schema/template.v1.json a plugin can check with jq.
scan() {
  local src dir base p d f shown
  for src in "${SOURCES[@]}"; do
    dir=$(source_dir "$src")
    [ -d "$dir/.git" ] || continue
    shown=$(show "$src")
    for p in "${SEARCH_PATHS[@]}"; do
      base=$dir
      [ "$p" = . ] || base="$dir/$p"
      for d in "$base"/*/; do
        d=${d%/}
        f="$d/template.json"
        [ ! -L "$d" ] && [ -f "$f" ] && [ ! -L "$f" ] || continue
        jq -cs --arg source "$shown" --arg dir "$d" '
          def isid: type == "string" and test("^[a-z][a-z0-9-]{1,31}$");
          def istext: type == "string" and length >= 1 and length <= 500;
          def isline: type == "string" and length >= 1 and (test("[\\n\\r]") | not);
          def islines: type == "array" and all(.[]; isline);
          def extra($where; $allowed): (keys - $allowed) | map($where + "unknown key " + .);
          def part($key; f): if has($key) then (.[$key] | f) else [] end;
          def problems:
            if type != "object" then ["template.json is not an object"] else
              extra(""; ["$schema", "$comment", "id", "name", "description", "language", "tags", "params", "files", "hooks", "requires"])
              + (if (.id | isid) then [] else ["id is missing or not a valid ID"] end)
              + (if (.name | istext) then [] else ["name is missing or not a text of 1 to 500 characters"] end)
              + part("$schema"; if type == "string" then [] else ["$schema is not a text"] end)
              + part("$comment"; if type == "string" then [] else ["$comment is not a text"] end)
              + part("description"; if istext then [] else ["description is not a text of 1 to 500 characters"] end)
              + part("language"; if type == "string" and test("^[a-z][a-z0-9-]*$") then [] else ["language is not valid"] end)
              + part("tags"; if islines then [] else ["tags is not a list of texts"] end)
              + part("params";
                  if type != "object" then ["params is not an object"] else
                    [to_entries[] | .key as $k | .value as $f
                      | (if ($k | test("^[a-z][A-Za-z0-9]{0,47}$")) then empty else "params: key " + $k + " is not valid" end),
                        (if ($f | type == "object" and ((.type? // "") | IN("string", "integer", "number", "boolean", "array", "object")) and (.label | istext))
                         then empty else "params." + $k + ": type and label are required" end)]
                  end)
              + part("files";
                  if type != "object" then ["files is not an object"] else
                    extra("files: "; ["include", "exclude", "render"])
                    + [to_entries[] | select(.value | islines | not) | "files." + .key + " is not a list of globs"]
                  end)
              + part("hooks";
                  if type != "object" then ["hooks is not an object"] else
                    extra("hooks: "; ["postCreate"])
                    + part("postCreate";
                        if type == "array" and all(.[]; type == "array" and length >= 1 and all(.[]; type == "string" and (test("[\\n\\r]") | not)) and (.[0] | length >= 1))
                        then [] else ["hooks.postCreate is not a list of commands, each a list of texts"] end)
                  end)
              + part("requires";
                  if type != "object" then ["requires is not an object"] else
                    extra("requires: "; ["commands", "plugins"])
                    + part("commands"; if islines then [] else ["requires.commands is not a list of command names"] end)
                    + part("plugins";
                        if type == "object" and all(to_entries[]; (.key | isid) and (.value | type == "string"))
                        then [] else ["requires.plugins is not an object of plugin IDs with version ranges"] end)
                  end)
            end;
          if length == 1 then .[0] | {source: $source, dir: $dir, errors: problems, template: .}
          else {source: $source, dir: $dir, errors: ["template.json is not one JSON object"], template: null} end' "$f" 2>/dev/null ||
          jq -cn --arg source "$shown" --arg dir "$d" \
            '{source: $source, dir: $dir, errors: ["template.json is not valid JSON"], template: null}'
      done
    done
  done
}

# find_template ID: the folder of the first usable template with this ID; prints
# nothing when there is none.
find_template() {
  scan | jq -rs --arg id "$1" 'map(select(.errors == [] and .template.id == $id)) | first // empty | .dir'
}

# project_dir NAME: the folder of a project.
project_dir() { printf '%s/%s' "$PROJECT_ROOT" "$1"; }

# valid_name NAME: whether NAME is usable as the name of a project folder.
valid_name() { [[ $1 =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$ ]]; }

# fail CODE TEXT: reports an error and ends the action with an exit code of the protocol.
fail() {
  sdk emit log error "$2"
  exit "$1"
}
