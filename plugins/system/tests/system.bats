#!/usr/bin/env bats
# Offline tests of the system plugin. The self-update against a fake release list and
# the environment loader in a fake HOME run in test/testscript/system.txtar.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks return valid results without network" {
  # An unreachable release list: a development build never asks for it, a released
  # build reports the problem as a check result.
  run devbox plugin test --checks-only --set system.releaseUrl=http://127.0.0.1:1/releases "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"system.host"*"ok"* ]]
  [[ "$output" == *"system.version"* ]]
}

# refute CMD…: fails the test when the command succeeds (a bare "!" would not).
refute() {
  if "$@"; then
    echo "unexpected success: $*" >&2
    return 1
  fi
}

@test "version helpers order semantic versions" {
  export DEVBOX_BIN=devbox
  source "$BATS_TEST_DIRNAME/../lib/common.sh"
  version_newer 0.10.0 0.9.0
  version_newer 1.0.0 1.0.0-rc.1
  version_newer 1.0.0-rc.2 1.0.0-rc.1
  version_newer v1.2.3 1.2.2
  refute version_newer 1.0.0-rc.1 1.0.0
  refute version_newer 1.2.3 1.2.3
  refute version_newer 0.9.0 0.10.0
}

@test "development builds are told apart from releases" {
  export DEVBOX_BIN=devbox
  source "$BATS_TEST_DIRNAME/../lib/common.sh"
  is_release_version 1.2.3
  is_release_version v1.2.3-rc.1
  refute is_release_version 0.0.0-dev
  refute is_release_version abc1234
  refute is_release_version v1.2.3-4-gabc1234
  refute is_release_version v1.2.3-dirty
}
