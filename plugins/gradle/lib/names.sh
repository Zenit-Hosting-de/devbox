#!/usr/bin/env bash
# Pure functions of the gradle plugin: property names, repository addresses and
# escaping. Sourced by common.sh; needs nothing from the environment.

# camel NAME: NAME in lowerCamelCase, hyphens and other separators dropped
# (team-registry -> teamRegistry).
camel() {
  local in=$1 out="" up=false c i
  for ((i = 0; i < ${#in}; i++)); do
    c=${in:i:1}
    if [[ $c != [A-Za-z0-9] ]]; then
      up=true
      continue
    fi
    if $up && [ -n "$out" ]; then c=${c^}; fi
    up=false
    out+=$c
  done
  printf '%s' "$out"
}

# prop_base PREFIX NAME: the start of the property names of one repository. Without
# a prefix it is NAME itself, otherwise PREFIX followed by NAME with a capital letter.
prop_base() {
  local prefix=$1 name=$2
  if [ -z "$prefix" ]; then
    printf '%s' "$name"
  else
    printf '%s%s' "$prefix" "${name^}"
  fi
}

# repo_url LAYOUT BASE REPOSITORY URL: the Maven repository address of a credential,
# or nothing when neither the option url nor repository is set. A full URL wins;
# otherwise the layout builds it. Prints an error and returns 2 when the layout cannot.
repo_url() {
  local layout=$1 base=${2%/} repo=$3 url=$4
  if [ -n "$url" ]; then
    printf '%s' "$url"
    return 0
  fi
  [ -n "$repo" ] || return 0
  case "$layout" in
  nexus) printf '%s/repository/%s/' "$base" "$repo" ;;
  artifactory) printf '%s/artifactory/%s/' "$base" "$repo" ;;
  *)
    echo "layout $layout needs the option url with the full repository address" >&2
    return 2
    ;;
  esac
}

# prop_escape TEXT: TEXT as the value of a Java properties line (backslashes, line
# breaks, tabs and a leading blank escaped).
prop_escape() {
  local s=${1//\\/\\\\}
  s=${s//$'\n'/\\n}
  s=${s//$'\r'/\\r}
  s=${s//$'\t'/\\t}
  [ "${s:0:1}" != " " ] || s="\\$s"
  printf '%s' "$s"
}

# shell_quote TEXT: TEXT in single quotes for a shell file.
shell_quote() {
  local s=${1//\'/\'\\\'\'}
  printf "'%s'" "$s"
}
