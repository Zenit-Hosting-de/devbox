#!/usr/bin/env bash
# Action: sets user.name and user.email in a marked block of the Git configuration.
# An empty parameter keeps the value DevBox set before; both empty change nothing.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

block="git:identity"
name=$(sdk param name)
email=$(sdk param email)
[[ $name != *$'\n'* ]] || fail "the name must be one line"
if [ -n "$email" ] && ! [[ $email =~ ^[^[:space:]\<\>@]+@[^[:space:]\<\>@]+$ ]]; then
  fail "not an e-mail address: $email"
fi

if [ -z "$name" ] && [ -z "$email" ]; then
  sdk emit log info "no name or e-mail address given; the identity stays as it is"
  sdk emit result "Identity unchanged"
  exit 0
fi

[ -n "$name" ] || name=$(block_value "$GITCONFIG" "$block" name)
[ -n "$email" ] || email=$(block_value "$GITCONFIG" "$block" email)

text=$(gc_header user)$'\n'
[ -z "$name" ] || text+=$'\t'"name = $(gc_quote "$name")"$'\n'
[ -z "$email" ] || text+=$'\t'"email = $(gc_quote "$email")"$'\n'

sdk emit step identity running "user.name, user.email"
mark_created "$GITCONFIG"
printf '%s' "$text" | sdk block "$GITCONFIG" "$block"
sdk emit step identity "done" "user.name, user.email"

summary=$name
[ -z "$email" ] || summary+="${summary:+ }<$email>"
sdk emit result "Identity $summary" "{\"name\":$(json_str "$name"),\"email\":$(json_str "$email")}"
