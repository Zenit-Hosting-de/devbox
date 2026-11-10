#!/usr/bin/env bats
# Tests of the gradle plugin. The consumer scripts run here directly with a request
# file; the full flow through the vault and the fake registry (verify, apply, remove)
# runs in test/testscript/gradle.txtar.

bats_require_minimum_version 1.5.0

setup() {
  PLUGIN="$BATS_TEST_DIRNAME/.."
  PROPS="$BATS_TEST_TMPDIR/gradle.properties"
  ENVF="$BATS_TEST_TMPDIR/env.d/gradle.sh"
  MSGS="$BATS_TEST_TMPDIR/messages.jsonl"
}

# request KIND MODE OPTIONS: writes a consumer request for the instance team-registry.
request() {
  cat >"$BATS_TEST_TMPDIR/request.json" <<JSON
{
  "protocol": 1, "kind": "$1", "plugin": "gradle", "name": "registry", "dryRun": false,
  "host": { "distro": "debian", "distroVersion": "12", "arch": "amd64" },
  "settings": { "mode": "$2", "propertyPrefix": "", "propertiesPath": "$PROPS", "envFile": "$ENVF" },
  "credential": {
    "instance": "team-registry", "type": "registry",
    "fields": { "url": "https://nexus.example.com", "layout": "nexus", "username": "alice" },
    "options": $3,
    "secret": "s3cr\\\\et"
  }
}
JSON
}

# consume SCRIPT [DRY]: runs consume/SCRIPT.sh with the request; messages go to $MSGS.
consume() {
  DEVBOX_BIN=$(command -v devbox) DEVBOX_PLUGIN_DIR="$PLUGIN" DEVBOX_REQUEST="$BATS_TEST_TMPDIR/request.json" \
    DEVBOX_MSG_FD=3 DEVBOX_DRY_RUN="${2:-0}" "$PLUGIN/consume/$1.sh" 3>"$MSGS"
}

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$PLUGIN"
}

@test "the check prints one valid result" {
  HOME="$BATS_TEST_TMPDIR" run devbox plugin test --checks-only "$PLUGIN"
  [ "$status" -eq 0 ]
  [[ "$output" == *"gradle.config"*"ok"* ]]
}

@test "file mode: dry run changes nothing, apply is idempotent, remove restores the file" {
  printf 'org.gradle.caching=true\n' >"$PROPS"
  cp "$PROPS" "$BATS_TEST_TMPDIR/before"
  request apply file '{ "repository": "maven-public" }'
  consume apply 1
  cmp "$PROPS" "$BATS_TEST_TMPDIR/before"
  grep -q '"type":"change"' "$MSGS"
  consume apply
  grep -qx 'teamRegistryUsername=alice' "$PROPS"
  grep -qx 'teamRegistryPassword=s3cr\\\\et' "$PROPS"
  grep -qx 'teamRegistryUrl=https://nexus.example.com/repository/maven-public/' "$PROPS"
  [ "$(stat -c %a "$PROPS")" = 600 ]
  consume apply
  run ! grep -q '"type":"change"' "$MSGS"
  request remove file '{}'
  consume remove
  cmp "$PROPS" "$BATS_TEST_TMPDIR/before"
  [ ! -e "$ENVF" ]
}

@test "env mode: exports in a new environment file, removed with the credential" {
  request apply env '{}'
  consume apply
  [ ! -e "$PROPS" ]
  grep -qx "export ORG_GRADLE_PROJECT_teamRegistryPassword='s3cr\\\\et'" "$ENVF"
  run ! grep -q 'teamRegistryUrl' "$ENVF"
  [ "$(stat -c %a "$ENVF")" = 600 ]
  consume apply
  run ! grep -q '"type":"change"' "$MSGS"
  request remove env '{}'
  consume remove
  [ ! -e "$ENVF" ]
}

@test "names, addresses and escaping" {
  # shellcheck source=../lib/names.sh
  . "$PLUGIN/lib/names.sh"
  [ "$(camel team-registry)" = teamRegistry ]
  [ "$(camel reg2-a-b)" = reg2AB ]
  [ "$(prop_base "" teamRegistry)" = teamRegistry ]
  [ "$(prop_base corp teamRegistry)" = corpTeamRegistry ]
  [ "$(repo_url nexus https://nexus.example.com/ maven-public "")" = https://nexus.example.com/repository/maven-public/ ]
  [ "$(repo_url artifactory https://art.example.com libs "")" = https://art.example.com/artifactory/libs/ ]
  [ "$(repo_url gitlab https://x.example.com "" https://maven.example.com/r)" = https://maven.example.com/r ]
  [ -z "$(repo_url gitlab https://x.example.com "" "")" ]
  run repo_url gitea https://x.example.com acme ""
  [ "$status" -eq 2 ]
  [ "$(prop_escape ' a\b')" = '\ a\\b' ]
  [ "$(shell_quote "a'b")" = "'a'\\''b'" ]
}
