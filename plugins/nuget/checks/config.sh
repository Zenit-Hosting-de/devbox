#!/usr/bin/env bash
# Check: package sources in NuGet.Config, and which of them DevBox keeps.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

details="[{\"label\":\"NuGet.Config\",\"value\":$(json_str "$CONFIG")}]"
if [ -e "$CONFIG" ] && [ ! -r "$CONFIG" ]; then
  printf '{"status":"error","summary":%s,"data":{"mode":%s,"configured":false,"sources":[]},"details":%s}\n' \
    "$(json_str "cannot read $CONFIG")" "$(json_str "$MODE")" "$details"
  exit 0
fi
if [ ! -f "$CONFIG" ]; then
  # No file: NuGet creates it with its default source on first use.
  summary="No NuGet.Config"
  [ -z "$DEFAULT_SOURCE" ] || summary="Default source $DEFAULT_SOURCE_NAME"
  printf '{"status":"ok","summary":%s,"data":{"mode":%s,"configured":false,"sources":[]},"details":%s}\n' \
    "$(json_str "$summary")" "$(json_str "$MODE")" "$details"
  exit 0
fi

# The <add> elements inside <packageSources>, outside of XML comments. A source inside
# a block devbox:nuget:NAME belongs to the credential NAME.
items=""
names=""
in_sources=false
in_comment=false
owner=""
add_re='<add[[:space:]]+key="([^"]*)"[[:space:]]+value="([^"]*)"'
begin_re='^<!-- >>> devbox:nuget:([a-z0-9-]+) >>> -->$'
end_re='^<!-- <<< devbox:nuget:([a-z0-9-]+) <<< -->$'
while IFS= read -r line || [ -n "$line" ]; do
  line=${line%$'\r'}
  t=${line#"${line%%[![:space:]]*}"}
  t=${t%"${t##*[![:space:]]}"}
  if [[ $t =~ $begin_re ]]; then
    owner=${BASH_REMATCH[1]}
    continue
  fi
  if [[ $t =~ $end_re ]]; then
    owner=""
    continue
  fi
  if $in_comment; then
    [[ $t != *'-->'* ]] || in_comment=false
    continue
  fi
  if [[ $t == '<!--'* ]]; then
    [[ $t == *'-->'* ]] || in_comment=true
    continue
  fi
  case "$t" in
  '<packageSources>'*) in_sources=true ;;
  '</packageSources>'*) in_sources=false ;;
  esac
  if $in_sources && [[ $t =~ $add_re ]]; then
    key=${BASH_REMATCH[1]}
    value=${BASH_REMATCH[2]}
    cred="null"
    [ -z "$owner" ] || cred=$(json_str "$owner")
    items+="${items:+,}{\"name\":$(json_str "$key"),\"url\":$(json_str "$value"),\"credential\":$cred}"
    names+="${names:+, }$key"
  fi
done <"$CONFIG"

if [ -z "$names" ]; then
  printf '{"status":"warn","summary":"No package source","data":{"mode":%s,"configured":true,"sources":[]},"details":%s,"hints":[{"text":"NuGet finds no package without a source in NuGet.Config."}]}\n' \
    "$(json_str "$MODE")" "$details"
else
  printf '{"status":"ok","summary":%s,"data":{"mode":%s,"configured":true,"sources":[%s]},"details":%s}\n' \
    "$(json_str "$names")" "$(json_str "$MODE")" "$items" "$details"
fi
