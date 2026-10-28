#!/usr/bin/env bash
# Pure functions of the npm plugin: registry addresses per layout, npmrc keys and
# variable names. Sourced by common.sh; needs nothing from the environment.

# slash URL: URL with exactly one trailing slash.
slash() {
  local u=$1
  while [ "${u%/}" != "$u" ]; do u=${u%/}; done
  printf '%s/' "$u"
}

# registry_url LAYOUT BASE REPOSITORY URL: the registry address of a credential. A full
# URL wins; otherwise the layout builds it from the base address and the repository.
# Prints an error and returns 2 when the options do not suffice.
registry_url() {
  local layout=$1 base=${2%/} repo=$3 url=$4
  if [ -n "$url" ]; then
    slash "$url"
    return 0
  fi
  case "$layout" in
  nexus | artifactory | gitea)
    if [ -z "$repo" ]; then
      echo "layout $layout needs the option repository (or url)" >&2
      return 2
    fi
    case "$layout" in
    nexus) slash "$base/repository/$repo" ;;
    artifactory) slash "$base/artifactory/api/npm/$repo" ;;
    gitea) slash "$base/api/packages/$repo/npm" ;;
    esac
    ;;
  gitlab)
    # A numeric repository is a project id; without one the instance-level API is used.
    case "$repo" in
    '') slash "$base/api/v4/packages/npm" ;;
    *[!0-9]*)
      echo "layout gitlab: repository must be a project id, using the instance-level address" >&2
      slash "$base/api/v4/packages/npm"
      ;;
    *) slash "$base/api/v4/projects/$repo/packages/npm" ;;
    esac
    ;;
  *)
    echo "layout $layout needs the option url (full address)" >&2
    return 2
    ;;
  esac
}

# auth_key URL: the npmrc key prefix of a registry URL ("//host/path/").
auth_key() {
  local u=${1#*:}
  slash "$u"
}

# var_name INSTANCE: the environment variable that carries the token of an instance.
var_name() {
  local n=${1^^}
  n=${n//[^A-Z0-9]/_}
  printf 'NPM_TOKEN_%s' "$n"
}

# shell_quote TEXT: TEXT in single quotes for a shell file.
shell_quote() {
  local s=${1//\'/\'\\\'\'}
  printf "'%s'" "$s"
}
