#!/usr/bin/env bats
# Offline tests of the maven plugin. The full test against a fake download server
# (install, update, consumer apply and remove, idempotency) runs in
# test/testscript/maven.txtar.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks return valid results without network" {
  run devbox plugin test --checks-only "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"maven.installed"*"missing"* ]]
  [[ "$output" == *"maven.latest"*"ok"* ]]
}
