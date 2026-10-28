#!/usr/bin/env bats
# Runs the checks and dry runs of all actions in a sandbox, then applies the actions
# twice and expects no change on the second run.

@test "manifest and scripts are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks, dry runs and idempotency" {
  devbox plugin test --apply "$BATS_TEST_DIRNAME/.."
}
