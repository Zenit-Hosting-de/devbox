#!/usr/bin/env bash
# Consumer: makes Git over https authenticate at a git-host instance.
#   - marked block in the Git configuration: [credential "https://host"] that resets the
#     helpers of the user for this host and names the helper of the setting
#     credentialHelper, plus the user name of the instance
#   - the token goes to that helper through "git credential approve" (stdin only); with
#     the default helper "store" it lands in the file of the setting credentialsFile
#     (mode 0600)
# Idempotent: a token the helper already has is not written again.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
[[ $name =~ ^[a-z][a-z0-9-]{1,31}$ ]] || fail "invalid instance name $name"
url=$(sdk field url)
user=$(sdk field username 2>/dev/null) || user=""
protocol=$(sdk option protocol 2>/dev/null) || protocol=https
[ "$protocol" = https ] || fail "protocol $protocol is not supported; tokens go to Git over https only"
origin=$(origin_of "$url") || fail "the address $url of $name is not https://host; tokens go to Git over https only"
[ -n "$user" ] || fail "the credential $name has no user name, which Git sends with the token"
[[ $user != *$'\n'* ]] || fail "the user name of $name must be one line"
host=${origin#https://}
spec=$(helper_spec)
block="git:credential:$name"
secret=$(sdk secret)
[ -n "$secret" ] || fail "the credential $name has no token"
[[ $secret != *$'\n'* ]] || fail "the token of $name must be one line"

sdk emit step "apply-$name" running "$origin"

# A changed address, user name or helper: the token stored for the old ones goes first.
if has_block "$GITCONFIG" "$block"; then
  old_origin=$(block_origin "$GITCONFIG" "$block")
  old_user=$(block_value "$GITCONFIG" "$block" username)
  old_spec=$(block_value "$GITCONFIG" "$block" helper)
  if [ -n "$old_origin" ] && [ -n "$old_spec" ] &&
    { [ "$old_origin" != "$origin" ] || [ "$old_user" != "$user" ] || [ "$old_spec" != "$spec" ]; }; then
    forget "$old_spec" "${old_origin#https://}" "$old_user"
  fi
fi

text=$(gc_header credential "$origin")$'\n'
text+=$'\t'"helper ="$'\n'
text+=$'\t'"helper = $(gc_quote "$spec")"$'\n'
text+=$'\t'"username = $(gc_quote "$user")"$'\n'
mark_created "$GITCONFIG"
printf '%s' "$text" | sdk block "$GITCONFIG" "$block"

file=$(store_file "$spec") || file=""
current=$(stored_secret "$spec" "$host" "$user")
if [ "$current" != "$secret" ]; then
  if [ -n "$file" ]; then
    op=update
    [ -e "$file" ] || op=create
  fi
  if ! is_dry_run; then
    if [ -n "$file" ]; then
      mkdir -p "${file%/*}"
      backup_file "$file"
    fi
    # printf is a shell builtin: the token is in no argument of a process
    printf 'protocol=https\nhost=%s\nusername=%s\npassword=%s\n\n' "$host" "$user" "$secret" | git_cred "$spec" approve
    [ "$(stored_secret "$spec" "$host" "$user")" = "$secret" ] || {
      sdk emit log error "the credential helper $HELPER did not keep the token for $origin"
      exit 1
    }
  fi
  if [ -n "$file" ]; then
    sdk emit change file "$op" "$file" "token for $origin"
  else
    sdk emit change other update "git credential $HELPER" "token for $origin"
  fi
fi
if [ -n "$file" ] && ! is_dry_run; then
  secure_file "$file"
fi

sdk emit step "apply-$name" "done" "$origin"
sdk emit result "Git uses the credential $name for $origin"
