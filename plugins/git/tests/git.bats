#!/usr/bin/env bats
# Tests of the git plugin without network: manifest, plugin test (checks, dry run,
# repeat), the pure helpers, and the git-host consumer run directly with a request file
# in a temporary HOME. The flow through the CLI against a fake Git host over https runs
# in test/testscript/git.txtar.

PLUGIN="$BATS_TEST_DIRNAME/.."

setup() {
  export HOME="$BATS_TEST_TMPDIR/home"
  mkdir -p "$HOME/.config/devbox"
  MSGS="$BATS_TEST_TMPDIR/messages.jsonl"
  : >"$MSGS"
}

# request KIND NAME DRYRUN [CREDENTIAL_JSON]: writes a request file like the runner does.
request() {
  local cred=${4:-null} dry=false defaults=${S_DEFAULTS:-"{}"}
  [ "$3" != 1 ] || dry=true
  REQ="$BATS_TEST_TMPDIR/request.json"
  cat >"$REQ" <<EOF
{
  "protocol": 1, "kind": "$1", "plugin": "git", "name": "$2", "dryRun": $dry,
  "params": { "name": "${P_NAME:-}", "email": "${P_EMAIL:-}" },
  "settings": {
    "userName": "", "userEmail": "",
    "defaults": $defaults,
    "credentialHelper": "${S_HELPER:-store}",
    "gitconfig": "$HOME/.gitconfig",
    "credentialsFile": "$HOME/.config/devbox/git-credentials"
  },
  "credential": $cred,
  "host": { "distro": "ubuntu", "distroVersion": "24.04", "arch": "amd64" }
}
EOF
}

# cred URL USER SECRET: the credential part of a consumer request.
cred() {
  printf '{"instance":"corp","type":"git-host","fields":{"url":"%s","flavor":"gitlab","username":"%s"},"options":{"protocol":"https"},"secret":"%s"}' "$1" "$2" "$3"
}

# plugin SCRIPT [DRYRUN]: runs a script of the plugin with the protocol environment.
plugin() {
  DEVBOX_BIN=$(command -v devbox) DEVBOX_PLUGIN_DIR="$PLUGIN" DEVBOX_REQUEST="$REQ" \
    DEVBOX_MSG_FD=3 DEVBOX_DRY_RUN="${2:-0}" DEVBOX_BACKUP_DIR="$BATS_TEST_TMPDIR/backup" \
    "$PLUGIN/$1" 3>>"$MSGS"
}

changes() { grep -c '"type":"change"' "$MSGS" || true; }

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$PLUGIN"
}

@test "checks print JSON, dry runs change nothing, a second run changes nothing" {
  run devbox plugin test --apply \
    --set 'git.userName=Jane Doe' --set git.userEmail=jane@example.com \
    --set 'git.defaults={"init.defaultBranch":"main","url.https://git.example.com/.insteadOf":"ssh://git.example.com/"}' \
    "$PLUGIN"
  echo "$output"
  [ "$status" -eq 0 ]
  [[ "$output" == *"check"*"git.config"* ]]
  [[ "$output" == *"dry-run"*"git.identity"*"2 planned changes"* ]]
  [[ "$output" == *"repeat"*"git.identity"*"no changes"* ]]
  [[ "$output" == *"repeat"*"git.defaults"*"no changes"* ]]
  [[ "$output" != *"failed"* ]]
}

@test "without a name and e-mail address the identity stays as it is" {
  run devbox plugin test --apply "$PLUGIN"
  [ "$status" -eq 0 ]
  [[ "$output" == *"apply"*"git.identity"*"0 changes"* ]]
}

@test "values are quoted for Git and read back" {
  . "$PLUGIN/lib/gitconfig.sh"
  run gc_quote 'a "b" \c'
  [ "$output" = '"a \"b\" \\c"' ]
  run gc_unquote '"a \"b\" \\c"'
  [ "$output" = 'a "b" \c' ]
  run gc_header credential 'https://h.example.com'
  [ "$output" = '[credential "https://h.example.com"]' ]
  run json_str $'x"y\\z\n'
  [ "$output" = '"x\"y\\z\n"' ]
  run json_pointer_token 'url.https://a~b/.insteadOf'
  [ "$output" = 'url.https:~1~1a~0b~1.insteadOf' ]
}

@test "config keys are split into section, subsection and variable" {
  . "$PLUGIN/lib/gitconfig.sh"
  run gc_entry init.defaultBranch main
  [ "$output" = $'[init]\n\tdefaultBranch = "main"' ]
  run gc_entry url.https://git.example.com/.insteadOf ssh://git.example.com/
  [ "$output" = $'[url "https://git.example.com/"]\n\tinsteadOf = "ssh://git.example.com/"' ]
  run gc_entry pull.ff only '[pull]'
  [ "$output" = $'\tff = "only"' ]
  run gc_entry nodot x
  [ "$status" -eq 2 ]
  run gc_entry a..b x
  [ "$status" -eq 2 ]
  run gc_entry 'a.b c' x
  [ "$status" -eq 2 ]
}

@test "credential addresses are https origins without user or path" {
  . "$PLUGIN/lib/gitconfig.sh"
  run origin_of https://u:p@Git.Example.com:8443/group/repo.git
  [ "$output" = "https://git.example.com:8443" ]
  run origin_of https://git.example.com
  [ "$output" = "https://git.example.com" ]
  run origin_of http://git.example.com
  [ "$status" -eq 2 ]
  run origin_of https:///path
  [ "$status" -eq 2 ]
}

@test "consumer: apply writes block and token, a second run changes nothing, remove restores the file" {
  printf '[credential]\n\thelper = cache\n[core]\n\teditor = vi\n' >"$HOME/.gitconfig"
  cp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/before"
  request apply git-host 0 "$(cred https://git.example.com/group oauth2 's3cr%t@x')"

  # dry run: nothing changes, the planned changes are reported
  plugin consume/apply.sh 1
  cmp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/before"
  [ ! -e "$HOME/.config/devbox/git-credentials" ]
  [ "$(changes)" -eq 2 ]

  : >"$MSGS"
  plugin consume/apply.sh
  grep -qx '# >>> devbox:git:credential:corp >>>' "$HOME/.gitconfig"
  grep -qx '\[credential "https://git.example.com"\]' "$HOME/.gitconfig"
  grep -qx $'\thelper =' "$HOME/.gitconfig"
  grep -qx $'\tusername = "oauth2"' "$HOME/.gitconfig"
  grep -qx 'https://oauth2:s3cr%25t%40x@git.example.com' "$HOME/.config/devbox/git-credentials"
  [ "$(stat -c %a "$HOME/.config/devbox/git-credentials")" = 600 ]
  ! grep -q 's3cr' "$MSGS"
  ! grep -q 's3cr' "$HOME/.gitconfig"
  # git itself finds the token through the configuration
  out=$(printf 'protocol=https\nhost=git.example.com\n\n' | GIT_TERMINAL_PROMPT=0 git credential fill)
  [[ $out == *$'\npassword=s3cr%t@x'* ]]

  # second run and a dry run: no change
  cp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/applied"
  : >"$MSGS"
  plugin consume/apply.sh
  plugin consume/apply.sh 1
  [ "$(changes)" -eq 0 ]
  cmp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/applied"

  # the check sees the host and its token
  request check config 0
  run plugin checks/config.sh
  [[ "$output" == *'"hosts":[{"instance":"corp","url":"https://git.example.com","username":"oauth2"'*'"stored":true}]'* ]]
  [[ "$output" != *"s3cr"* ]]

  # remove: back to the file as it was, the token file is gone
  request remove git-host 0 "$(cred https://git.example.com/group oauth2 's3cr%t@x')"
  plugin consume/remove.sh 1
  cmp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/applied"
  plugin consume/remove.sh
  cmp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/before"
  [ ! -e "$HOME/.config/devbox/git-credentials" ]
  : >"$MSGS"
  plugin consume/remove.sh
  [ "$(changes)" -eq 0 ]
}

@test "consumer: a new token replaces the old one, a changed address drops the old host" {
  request apply git-host 0 "$(cred https://git.example.com oauth2 one)"
  plugin consume/apply.sh
  request apply git-host 0 "$(cred https://git.example.com oauth2 two)"
  : >"$MSGS"
  plugin consume/apply.sh
  [ "$(changes)" -eq 1 ]
  grep -qx 'https://oauth2:two@git.example.com' "$HOME/.config/devbox/git-credentials"
  ! grep -q ':one@' "$HOME/.config/devbox/git-credentials"
  request apply git-host 0 "$(cred https://other.example.com:8443 oauth2 two)"
  plugin consume/apply.sh
  grep -qx 'https://oauth2:two@other.example.com%3a8443' "$HOME/.config/devbox/git-credentials"
  ! grep -q '@git.example.com' "$HOME/.config/devbox/git-credentials"
  [ "$(grep -c 'devbox:git:credential:corp >>>' "$HOME/.gitconfig")" -eq 1 ]
  request check config 0
  run plugin checks/config.sh
  [[ "$output" == *'"url":"https://other.example.com:8443"'*'"stored":true'* ]]
  request apply git-host 0 "$(cred https://other.example.com:8443 oauth2 two)"
  ! grep -q 'credential "https://git.example.com"' "$HOME/.gitconfig"
  # remove takes the token from the block's address and deletes the file DevBox created
  request remove git-host 0 "$(cred https://git.example.com oauth2 two)"
  plugin consume/remove.sh
  [ ! -e "$HOME/.gitconfig" ]
  [ ! -e "$HOME/.config/devbox/git-credentials" ]
}

@test "consumer: plain http and missing user names are refused" {
  request apply git-host 0 "$(cred http://git.example.com oauth2 tok)"
  run plugin consume/apply.sh
  [ "$status" -eq 2 ]
  request apply git-host 0 "$(cred https://git.example.com '' tok)"
  run plugin consume/apply.sh
  [ "$status" -eq 2 ]
  [ ! -e "$HOME/.gitconfig" ]
}

@test "identity: an empty parameter keeps the value set before" {
  P_NAME="Jane Doe" P_EMAIL=jane@example.com request action identity 0
  plugin actions/identity.sh
  P_NAME="" P_EMAIL=jd@example.com request action identity 0
  plugin actions/identity.sh
  [ "$(git config --global user.name)" = "Jane Doe" ]
  [ "$(git config --global user.email)" = "jd@example.com" ]
  P_NAME="" P_EMAIL="not an address" request action identity 0
  run plugin actions/identity.sh
  [ "$status" -eq 2 ]
}

@test "defaults: replaced as a whole, removed when empty, invalid keys refused" {
  printf '[core]\n\teditor = vi\n' >"$HOME/.gitconfig"
  cp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/before"
  S_DEFAULTS='{"pull.rebase":"true","pull.ff":"only"}' request action defaults 0
  plugin actions/defaults.sh
  [ "$(git config --global pull.ff)" = only ]
  grep -c '^\[pull\]$' "$HOME/.gitconfig" | grep -qx 1
  S_DEFAULTS='{"pull.rebase":"true","pull.ff":"only"}' request check config 0
  run plugin checks/config.sh
  [[ "$output" == *'"applied":true'* ]]
  [[ "$output" != *'"applied":false'* ]]
  S_DEFAULTS='{"pull.rebase":"true"}' request action defaults 0
  plugin actions/defaults.sh
  ! git config --global pull.ff
  request action defaults 0
  plugin actions/defaults.sh
  cmp "$HOME/.gitconfig" "$BATS_TEST_TMPDIR/before"
  S_DEFAULTS='{"no-dot":"x"}' request action defaults 0
  run plugin actions/defaults.sh
  [ "$status" -eq 2 ]
}

@test "check: no identity is a warning with the action identity" {
  request check config 0
  run plugin checks/config.sh
  [ "$status" -eq 0 ]
  [[ "$output" == '{"status":"warn","summary":"No identity"'* ]]
  [[ "$output" == *'"action":"git.identity"'* ]]
}
