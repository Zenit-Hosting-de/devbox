#!/usr/bin/env bash
# Pure functions of the pip plugin: index addresses per layout, host names, encoding
# and quoting. Sourced by common.sh; needs nothing from the environment.

# strip_slashes URL: URL without trailing slashes.
strip_slashes() {
  local u=$1
  while [ "${u%/}" != "$u" ]; do u=${u%/}; done
  printf '%s' "$u"
}

# index_url LAYOUT BASE REPOSITORY URL: the simple index address of a credential. A
# full URL wins; otherwise the layout builds it from the base address and the
# repository. Prints an error and returns 2 when the options do not suffice.
index_url() {
  local layout=$1 base repo=$3 url=$4
  base=$(strip_slashes "$2")
  if [ -n "$url" ]; then
    strip_slashes "$url"
    return 0
  fi
  if [ -z "$repo" ]; then
    case "$layout" in
    nexus | artifactory | gitea | gitlab)
      echo "layout $layout needs the option repository (or url)" >&2
      return 2
      ;;
    esac
  fi
  case "$layout" in
  nexus) printf '%s/repository/%s/simple' "$base" "$repo" ;;
  artifactory) printf '%s/artifactory/api/pypi/%s/simple' "$base" "$repo" ;;
  gitea) printf '%s/api/packages/%s/pypi/simple' "$base" "$repo" ;;
  gitlab)
    case "$repo" in
    *[!0-9]*)
      echo "layout gitlab: repository must be a project id (or use the option url)" >&2
      return 2
      ;;
    esac
    printf '%s/api/v4/projects/%s/packages/pypi/simple' "$base" "$repo"
    ;;
  *)
    echo "layout $layout needs the option url (full address)" >&2
    return 2
    ;;
  esac
}

# url_host URL: the host name of URL, without user information and port.
url_host() {
  local h=${1#*://}
  h=${h%%/*}
  h=${h##*@}
  case "$h" in
  \[*) h=${h%%]*}] ;;
  *) h=${h%%:*} ;;
  esac
  printf '%s' "$h"
}

# strip_userinfo URL: URL without "user:password@".
strip_userinfo() {
  local u=$1 scheme rest auth
  case "$u" in
  *://*) ;;
  *)
    printf '%s' "$u"
    return 0
    ;;
  esac
  scheme=${u%%://*}
  rest=${u#*://}
  auth=${rest%%/*}
  case "$auth" in
  *@*) rest=${rest#"${auth%@*}@"} ;;
  esac
  printf '%s://%s' "$scheme" "$rest"
}

# pct_encode TEXT: TEXT with every byte outside the unreserved set of RFC 3986
# percent-encoded, for user names and secrets inside an address.
pct_encode() {
  local LC_ALL=C s=$1 out="" c hex i
  for ((i = 0; i < ${#s}; i++)); do
    c=${s:i:1}
    case "$c" in
    [A-Za-z0-9._~-]) out+=$c ;;
    *)
      printf -v hex '%%%02X' "'$c"
      out+=$hex
      ;;
    esac
  done
  printf '%s' "$out"
}

# url_with_auth URL USER SECRET: URL with the encoded user and secret in front of the
# host. Runs in the shell only, so the secret never appears in process arguments.
url_with_auth() {
  local scheme=${1%%://*} rest=${1#*://}
  printf '%s://%s:%s@%s' "$scheme" "$(pct_encode "$2")" "$(pct_encode "$3")" "$rest"
}

# netrc_safe TEXT: whether TEXT can stand as a single token in .netrc.
netrc_safe() {
  [[ -n $1 && $1 != *[[:space:]\"\\]* ]]
}

# shell_quote TEXT: TEXT in single quotes for a shell file.
shell_quote() {
  local s=${1//\'/\'\\\'\'}
  printf "'%s'" "$s"
}

# shell_unquote TEXT: the value of a single-quoted shell word written by shell_quote.
shell_unquote() {
  local s=$1
  s=${s#\'}
  s=${s%\'}
  printf '%s' "${s//\'\\\'\'/\'}"
}
