#!/usr/bin/env bash
# Pure functions of the nuget plugin: source names, feed addresses and escaping.
# Sourced by common.sh; needs nothing from the environment.

# source_key OPTION SETTING INSTANCE: the key of the package source. The option of the
# instance wins, then the setting sourceName, then the instance name with hyphens
# turned into underscores (NuGetPackageSourceCredentials_<key> must be a shell name).
source_key() {
  if [ -n "$1" ]; then
    printf '%s' "$1"
  elif [ -n "$2" ]; then
    printf '%s' "$2"
  else
    printf '%s' "${3//-/_}"
  fi
}

# feed_url LAYOUT BASE REPOSITORY URL: the feed address of a credential. A full URL
# wins; otherwise the layout builds it from the base address and the repository.
# Prints an error and returns 2 when the options do not suffice.
feed_url() {
  local layout=$1 base=${2%/} repo=$3 url=$4
  if [ -n "$url" ]; then
    printf '%s' "$url"
    return 0
  fi
  case "$layout" in
  nexus | artifactory) ;;
  *)
    echo "layout $layout needs the option url with the full feed address" >&2
    return 2
    ;;
  esac
  if [ -z "$repo" ]; then
    echo "layout $layout needs the option repository (or url)" >&2
    return 2
  fi
  case "$layout" in
  nexus) printf '%s/repository/%s/' "$base" "$repo" ;;
  artifactory) printf '%s/artifactory/api/nuget/%s/' "$base" "$repo" ;;
  esac
}

# xml_escape TEXT: TEXT for use in an XML attribute value.
xml_escape() {
  local s=$1 amp='&amp;' lt='&lt;' gt='&gt;' quot='&quot;' apos='&apos;'
  s=${s//&/"$amp"}
  s=${s//</"$lt"}
  s=${s//>/"$gt"}
  s=${s//\"/"$quot"}
  s=${s//\'/"$apos"}
  printf '%s' "$s"
}

# shell_quote TEXT: TEXT in single quotes for a shell file.
shell_quote() {
  local s=${1//\'/\'\\\'\'}
  printf "'%s'" "$s"
}
