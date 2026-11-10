#!/usr/bin/env bats
# Tests of the nuget plugin. The consumer scripts run here directly with a request
# file; the full flow through the vault and the fake registry (verify, apply, remove,
# well-formed XML) runs in test/testscript/nuget.txtar.

bats_require_minimum_version 1.5.0

setup() {
  PLUGIN="$BATS_TEST_DIRNAME/.."
  CONFIG="$BATS_TEST_TMPDIR/NuGet/NuGet.Config"
  ENVF="$BATS_TEST_TMPDIR/env.d/nuget.sh"
  MSGS="$BATS_TEST_TMPDIR/messages.jsonl"
}

# request KIND MODE OPTIONS: writes a consumer request for the instance team-registry.
request() {
  cat >"$BATS_TEST_TMPDIR/request.json" <<JSON
{
  "protocol": 1, "kind": "$1", "plugin": "nuget", "name": "registry", "dryRun": false,
  "host": { "distro": "debian", "distroVersion": "12", "arch": "amd64" },
  "settings": {
    "mode": "$2", "sourceName": "", "configPath": "$CONFIG", "envFile": "$ENVF",
    "defaultSource": "https://api.nuget.org/v3/index.json", "defaultSourceName": "nuget.org"
  },
  "credential": {
    "instance": "team-registry", "type": "registry",
    "fields": { "url": "https://nexus.example.com", "layout": "nexus", "username": "alice" },
    "options": $3,
    "secret": "s3<r&t"
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
  [[ "$output" == *"nuget.config"*"ok"* ]]
}

@test "file mode: dry run creates nothing, a new file has nuget.org and goes again on remove" {
  request apply file '{ "repository": "nuget-group" }'
  consume apply 1
  [ ! -e "$CONFIG" ]
  grep -q '"op":"create"' "$MSGS"
  consume apply
  grep -q '<add key="nuget.org" value="https://api.nuget.org/v3/index.json" protocolVersion="3" />' "$CONFIG"
  grep -q '<add key="team_registry" value="https://nexus.example.com/repository/nuget-group/" />' "$CONFIG"
  grep -q '<add key="ClearTextPassword" value="s3&lt;r&amp;t" />' "$CONFIG"
  [ "$(stat -c %a "$CONFIG")" = 600 ]
  consume apply
  run ! grep -q '"type":"change"' "$MSGS"
  request remove file '{}'
  consume remove
  [ ! -e "$CONFIG" ]
}

@test "file mode: an existing file without sections is restored byte for byte" {
  mkdir -p "${CONFIG%/*}"
  printf '<?xml version="1.0" encoding="utf-8"?>\n<configuration>\n  <config>\n    <add key="globalPackagesFolder" value="/data/nuget" />\n  </config>\n</configuration>\n' >"$CONFIG"
  cp "$CONFIG" "$BATS_TEST_TMPDIR/before"
  request apply file '{ "url": "https://nuget.example.com/v3/index.json", "sourceName": "corp" }'
  consume apply
  grep -q 'devbox:nuget-section:packageSources' "$CONFIG"
  grep -q '<add key="corp" value="https://nuget.example.com/v3/index.json" />' "$CONFIG"
  grep -q '<corp>' "$CONFIG"
  request remove file '{}'
  consume remove 1
  run ! cmp -s "$CONFIG" "$BATS_TEST_TMPDIR/before"
  consume remove
  cmp "$CONFIG" "$BATS_TEST_TMPDIR/before"
}

@test "env mode: the source in the file, the credential in the environment file" {
  request apply env '{ "repository": "nuget-group" }'
  consume apply
  run ! grep -q 'packageSourceCredentials' "$CONFIG"
  grep -qx "export NuGetPackageSourceCredentials_team_registry='Username=alice;Password=s3<r&t'" "$ENVF"
  [ "$(stat -c %a "$ENVF")" = 600 ]
  consume apply
  run ! grep -q '"type":"change"' "$MSGS"
  request remove env '{}'
  consume remove
  [ ! -e "$ENVF" ]
  [ ! -e "$CONFIG" ]
}

@test "names, addresses and escaping" {
  # shellcheck source=../lib/names.sh
  . "$PLUGIN/lib/names.sh"
  [ "$(source_key "" "" team-registry)" = team_registry ]
  [ "$(source_key "" corp team-registry)" = corp ]
  [ "$(source_key own corp team-registry)" = own ]
  [ "$(feed_url nexus https://nexus.example.com/ nuget-group "")" = https://nexus.example.com/repository/nuget-group/ ]
  [ "$(feed_url artifactory https://art.example.com nuget "")" = https://art.example.com/artifactory/api/nuget/nuget/ ]
  [ "$(feed_url gitlab https://x.example.com "" https://nuget.example.com/v3/index.json)" = https://nuget.example.com/v3/index.json ]
  run feed_url gitea https://x.example.com acme ""
  [ "$status" -eq 2 ]
  run feed_url nexus https://x.example.com "" ""
  [ "$status" -eq 2 ]
  [ "$(xml_escape "a&b<\"'>")" = 'a&amp;b&lt;&quot;&apos;&gt;' ]
  [ "$(shell_quote "a'b")" = "'a'\\''b'" ]
}
