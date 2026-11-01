#!/usr/bin/env bash
# Shared functions of the githost plugin. Sourced by actions.
# Uses only devbox sdk and the commands listed in requires.commands.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

# curl_quote TEXT: TEXT escaped for a double-quoted value in a curl config file.
curl_quote() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '%s' "$s"
}

# host_of URL: the host name of URL, lower case, without port.
host_of() {
  local h=${1#*://}
  h=${h%%/*}
  h=${h##*@}
  h=${h%%:*}
  printf '%s' "${h,,}"
}

# curl_config URL FLAVOR USERNAME SECRET: the curl config for one request. curl reads
# it from stdin (-K -), so the secret never appears on a command line. printf is a
# shell builtin, so no process gets the secret as an argument either.
curl_config() {
  local url=$1 flavor=$2 user=$3 secret=$4
  printf 'url = "%s"\n' "$(curl_quote "$url")"
  case "$flavor" in
  gitlab) printf 'header = "PRIVATE-TOKEN: %s"\n' "$(curl_quote "$secret")" ;;
  gitea) printf 'header = "Authorization: token %s"\n' "$(curl_quote "$secret")" ;;
  bitbucket) printf 'user = "%s:%s"\n' "$(curl_quote "$user")" "$(curl_quote "$secret")" ;;
  *) printf 'header = "Authorization: Bearer %s"\n' "$(curl_quote "$secret")" ;;
  esac
}
