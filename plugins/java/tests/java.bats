#!/usr/bin/env bats
# Offline tests of the java plugin. The full test against a release API (install,
# update, idempotency) runs in test/testscript/java.txtar with a fake API.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks return valid results without network" {
  run devbox plugin test --checks-only "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"java.installed"*"missing"* ]]
  [[ "$output" == *"java.latest"*"ok"* ]]
}
