#!/usr/bin/env bats
# Offline tests of the npm plugin. The consumer flows (file and env mode, remove,
# dry run) run in test/testscript/npm.txtar.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "checks and actions run, repeat without changes and change nothing in a dry run" {
  HOME="$BATS_TEST_TMPDIR" run devbox plugin test --apply "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"dry-run"*"npm.configure"* ]]
  [[ "$output" == *"repeat"*"npm.configure"*"no changes"* ]]
  [[ "$output" != *"failed"* ]]
}

@test "registry addresses follow the layout" {
  # shellcheck source=../lib/layout.sh
  . "$BATS_TEST_DIRNAME/../lib/layout.sh"
  run registry_url nexus https://nexus.example.com/ npm-group ""
  [ "$output" = "https://nexus.example.com/repository/npm-group/" ]
  run registry_url artifactory https://art.example.com npm-virtual ""
  [ "$output" = "https://art.example.com/artifactory/api/npm/npm-virtual/" ]
  run registry_url gitea https://gitea.example.com acme ""
  [ "$output" = "https://gitea.example.com/api/packages/acme/npm/" ]
  run registry_url gitlab https://gitlab.example.com 42 ""
  [ "$output" = "https://gitlab.example.com/api/v4/projects/42/packages/npm/" ]
  run registry_url gitlab https://gitlab.example.com "" ""
  [ "$output" = "https://gitlab.example.com/api/v4/packages/npm/" ]
  run registry_url custom https://x.example.com "" https://npm.example.com/registry
  [ "$output" = "https://npm.example.com/registry/" ]
}

@test "a layout without enough options is refused" {
  # shellcheck source=../lib/layout.sh
  . "$BATS_TEST_DIRNAME/../lib/layout.sh"
  run registry_url custom https://x.example.com "" ""
  [ "$status" -eq 2 ]
  run registry_url nexus https://x.example.com "" ""
  [ "$status" -eq 2 ]
}

@test "keys and variable names" {
  # shellcheck source=../lib/layout.sh
  . "$BATS_TEST_DIRNAME/../lib/layout.sh"
  run auth_key https://nexus.example.com:8081/repository/npm-group/
  [ "$output" = "//nexus.example.com:8081/repository/npm-group/" ]
  run var_name team-registry
  [ "$output" = "NPM_TOKEN_TEAM_REGISTRY" ]
  run shell_quote "a'b"
  [ "$output" = "'a'\\''b'" ]
}
