#!/usr/bin/env bats
# Offline tests of the python plugin. The full test against a fake uv installer
# (install, update, use, tools, remove, idempotency) runs in
# test/testscript/python.txtar.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks return valid results without network" {
  run devbox plugin test --checks-only "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"python.installed"*"missing"* ]]
  [[ "$output" == *"python.latest"*"ok"* ]]
}

@test "dry runs change no file before uv is installed" {
  run devbox plugin test "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"dry-run"*"python.install"*"planned changes"* ]]
  [[ "$output" == *"dry-run"*"python.update"*"0 planned changes"* ]]
  [[ "$output" != *"failed"* ]]
}
