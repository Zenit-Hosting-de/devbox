#!/usr/bin/env bats
# Offline tests of the base plugin. Install and update against a fake package
# manager run in test/testscript/base.txtar.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "the check returns a valid result on this machine" {
  run devbox plugin test --checks-only "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"base.packages"* ]]
}
