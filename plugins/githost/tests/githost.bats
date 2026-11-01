#!/usr/bin/env bats
# Offline tests of the githost plugin. Verification against a host runs in
# test/testscript/repo_credential.txtar with a fake host.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "verify without a credential has nothing to do and changes nothing" {
  run devbox plugin test --apply "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"dry-run"*"githost.verify"* ]]
  [[ "$output" == *"repeat"*"githost.verify"*"no changes"* ]]
  [[ "$output" != *"failed"* ]]
}

@test "the curl config carries the secret with the authentication of the flavor" {
  # shellcheck source=../lib/common.sh
  . "$BATS_TEST_DIRNAME/../lib/common.sh"
  run curl_config "https://gitlab.example.com/api/v4/user" gitlab oauth2 'p"w\d'
  [ "${lines[0]}" = 'url = "https://gitlab.example.com/api/v4/user"' ]
  [ "${lines[1]}" = 'header = "PRIVATE-TOKEN: p\"w\\d"' ]
  run curl_config "https://gitea.example.com/api/v1/user" gitea oauth2 tok
  [ "${lines[1]}" = 'header = "Authorization: token tok"' ]
  run curl_config "https://bitbucket.example.com/x" bitbucket alice tok
  [ "${lines[1]}" = 'user = "alice:tok"' ]
  run curl_config "https://api.github.com/user" github oauth2 tok
  [ "${lines[1]}" = 'header = "Authorization: Bearer tok"' ]
}

@test "the host name is taken from the address" {
  # shellcheck source=../lib/common.sh
  . "$BATS_TEST_DIRNAME/../lib/common.sh"
  [ "$(host_of "https://GitHub.com/")" = "github.com" ]
  [ "$(host_of "https://git.example.com:8443/x/y")" = "git.example.com" ]
  [ "$(host_of "https://user@git.example.com")" = "git.example.com" ]
}
