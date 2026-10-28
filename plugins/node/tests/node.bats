#!/usr/bin/env bats
# Offline tests of the node plugin. The full test against a distribution server
# (install, update, use, remove, global packages, idempotency) runs in
# test/testscript/node.txtar with a fake server.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks return valid results without network" {
  run devbox plugin test --checks-only "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"node.installed"*"missing"* ]]
  [[ "$output" == *"node.latest"*"ok"* ]]
}
