#!/usr/bin/env bats
# Offline tests of the dotnet plugin. The full test against release metadata and an
# install script (install, update, remove, workloads, idempotency) runs in
# test/testscript/dotnet.txtar with a fake server.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks return valid results without network" {
  run devbox plugin test --checks-only "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"dotnet.installed"*"missing"* ]]
  [[ "$output" == *"dotnet.latest"*"ok"* ]]
}
