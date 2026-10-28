#!/usr/bin/env bash
# Shared functions of the registry plugin. Sourced by actions.
# Uses only devbox sdk and the commands listed in requires.commands.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

# curl_quote TEXT: TEXT escaped for a double-quoted value in a curl config file.
curl_quote() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '%s' "$s"
}

# curl_config URL LAYOUT USERNAME SECRET: the curl config for one request. curl reads
# it from stdin (-K -), so the secret never appears on a command line. printf is a
# shell builtin, so no process gets the secret as an argument either.
curl_config() {
  local url=$1 layout=$2 user=$3 secret=$4
  printf 'url = "%s"\n' "$(curl_quote "$url")"
  if [ "$layout" = gitlab ]; then
    printf 'header = "PRIVATE-TOKEN: %s"\n' "$(curl_quote "$secret")"
  elif [ -n "$user" ]; then
    printf 'user = "%s:%s"\n' "$(curl_quote "$user")" "$(curl_quote "$secret")"
  elif [ "$layout" = gitea ]; then
    printf 'header = "Authorization: token %s"\n' "$(curl_quote "$secret")"
  else
    printf 'header = "Authorization: Bearer %s"\n' "$(curl_quote "$secret")"
  fi
}
