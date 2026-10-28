#!/usr/bin/env bats
# Offline tests of the registry plugin. Verification against a registry runs in
# test/testscript/registry.txtar with a fake registry server.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "verify without a credential has nothing to do and changes nothing" {
  run devbox plugin test --apply "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"dry-run"*"registry.verify"* ]]
  [[ "$output" == *"repeat"*"registry.verify"*"no changes"* ]]
  [[ "$output" != *"failed"* ]]
}

@test "the curl config carries the secret with the authentication of the layout" {
  # shellcheck source=../lib/common.sh
  . "$BATS_TEST_DIRNAME/../lib/common.sh"
  run curl_config "https://nexus.example.com/x" nexus alice 'p"w\d'
  [ "${lines[0]}" = 'url = "https://nexus.example.com/x"' ]
  [ "${lines[1]}" = 'user = "alice:p\"w\\d"' ]
  run curl_config "https://gitlab.example.com/api/v4/user" gitlab alice tok
  [ "${lines[1]}" = 'header = "PRIVATE-TOKEN: tok"' ]
  run curl_config "https://gitea.example.com/api/v1/user" gitea "" tok
  [ "${lines[1]}" = 'header = "Authorization: token tok"' ]
  run curl_config "https://registry.example.com/" custom "" tok
  [ "${lines[1]}" = 'header = "Authorization: Bearer tok"' ]
}
