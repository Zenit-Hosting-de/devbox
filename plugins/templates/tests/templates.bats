#!/usr/bin/env bats
# Tests of the templates plugin. The scripts run here directly with a request file and a
# local Git repository as the source (git+file://); the flow through the CLI runs in
# test/testscript/templates.txtar.

bats_require_minimum_version 1.5.0

setup() {
  PLUGIN="$BATS_TEST_DIRNAME/.."
  export HOME="$BATS_TEST_TMPDIR/home"
  REPO="$BATS_TEST_TMPDIR/repo"
  CACHE="$BATS_TEST_TMPDIR/cache"
  ROOT="$BATS_TEST_TMPDIR/projects"
  STATE="$BATS_TEST_TMPDIR/state"
  MSGS="$BATS_TEST_TMPDIR/messages.jsonl"
  SOURCES="[\"git+file://$REPO\"]"
  mkdir -p "$HOME" "$STATE"
  export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
  export GIT_AUTHOR_NAME=Test GIT_AUTHOR_EMAIL=test@example.com
  export GIT_COMMITTER_NAME=Test GIT_COMMITTER_EMAIL=test@example.com
  source_repo
}

# source_repo: a Git repository with a template in the root, one under templates/ and a
# folder whose template.json breaks the schema.
source_repo() {
  mkdir -p "$REPO/node-app/src" "$REPO/node-app/bin" "$REPO/templates/java-service/src/main/java" "$REPO/broken"
  cat >"$REPO/node-app/template.json" <<'JSON'
{
  "$schema": "https://devbox.zenit-hosting.de/schema/template.v1.json",
  "id": "node-app",
  "name": "Node application",
  "description": "Minimal HTTP service",
  "language": "node",
  "tags": ["http"],
  "params": {
    "projectName": { "type": "string", "label": "Project name", "pattern": "^[a-z][a-z0-9-]*$" },
    "port": { "type": "integer", "label": "Port", "default": 3000, "minimum": 1 },
    "license": { "type": "string", "label": "License", "enum": ["MIT", "Apache-2.0"], "default": "MIT" }
  },
  "files": {
    "exclude": ["**/.gitkeep"],
    "render": ["package.json", "README.md", "bin/*.sh"]
  }
}
JSON
  printf '{\n  "name": "{{projectName}}",\n  "license": "{{license}}",\n  "config": { "port": {{port}} }\n}\n' >"$REPO/node-app/package.json"
  printf '# {{projectName}}\n\nListens on {{port}}. {{unknown}} & {{ port }} stay.\n\n' >"$REPO/node-app/README.md"
  printf 'console.log("{{projectName}}");\n' >"$REPO/node-app/src/index.js"
  printf '#!/bin/sh\nexec node src/index.js --port {{port}}\n' >"$REPO/node-app/bin/run.sh"
  chmod 755 "$REPO/node-app/bin/run.sh"
  : >"$REPO/node-app/src/.gitkeep"
  cat >"$REPO/templates/java-service/template.json" <<'JSON'
{
  "id": "java-service",
  "name": "Java service",
  "language": "java",
  "params": { "groupId": { "type": "string", "label": "Group ID", "default": "com.example" } },
  "files": { "include": ["pom.xml", "src/**/*.java"], "render": ["pom.xml"] },
  "hooks": { "postCreate": [["touch", "hooked one.txt"], ["touch", "second.txt"]] }
}
JSON
  printf '<project><groupId>{{groupId}}</groupId></project>\n' >"$REPO/templates/java-service/pom.xml"
  printf 'class App {}\n' >"$REPO/templates/java-service/src/main/java/App.java"
  printf 'not part of the project\n' >"$REPO/templates/java-service/NOTES.txt"
  printf '{ "id": "Broken", "name": "Broken", "extra": true }\n' >"$REPO/broken/template.json"
  git -C "$REPO" init --quiet --initial-branch=main
  git -C "$REPO" add .
  git -C "$REPO" commit --quiet -m one
}

# request KIND NAME PARAMS: writes the request file with the settings of the test.
request() {
  cat >"$BATS_TEST_TMPDIR/request.json" <<JSON
{
  "protocol": 1, "kind": "$1", "plugin": "templates", "name": "$2", "dryRun": false,
  "host": { "distro": "debian", "distroVersion": "12", "arch": "amd64" },
  "params": $3,
  "settings": { "sources": $SOURCES, "searchPaths": [".", "templates"], "projectRoot": "$ROOT", "cacheDir": "$CACHE" }
}
JSON
}

# action NAME PARAMS [DRY]: runs actions/NAME.sh; messages go to $MSGS.
action() {
  request action "$1" "$2"
  DEVBOX_BIN=$(command -v devbox) DEVBOX_PLUGIN_DIR="$PLUGIN" DEVBOX_REQUEST="$BATS_TEST_TMPDIR/request.json" \
    DEVBOX_STATE_DIR="$STATE" DEVBOX_MSG_FD=3 DEVBOX_DRY_RUN="${3:-0}" "$PLUGIN/actions/$1.sh" 3>"$MSGS"
}

# available: runs the check and prints its result.
available() {
  request check available '{}'
  DEVBOX_BIN=$(command -v devbox) DEVBOX_PLUGIN_DIR="$PLUGIN" DEVBOX_REQUEST="$BATS_TEST_TMPDIR/request.json" \
    DEVBOX_DRY_RUN=0 "$PLUGIN/checks/available.sh"
}

changes() { grep -c '"type":"change"' "$MSGS" || true; }

# tree DIR: every file below DIR with its mode, sorted.
tree() { (cd "$1" && find . -type f -exec stat -c '%a %n' {} + | LC_ALL=C sort -k2); }

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$PLUGIN"
}

@test "plugin test: the check prints one valid result, the dry run of sync changes nothing" {
  run devbox plugin test "$PLUGIN"
  [ "$status" -eq 0 ]
  [[ "$output" == *"templates.available"*"missing"* ]]
  [[ "$output" == *"dry-run"*"templates.sync"*"1 planned changes"* ]]
}

@test "sync: dry run writes nothing, the first run fetches, the second changes nothing" {
  action sync '{}' 1
  [ ! -e "$CACHE" ]
  [ "$(changes)" -eq 1 ]

  action sync '{}'
  [ "$(changes)" -eq 1 ]
  copy=$(echo "$CACHE"/*/)
  [ -f "${copy}node-app/template.json" ]
  [ "$(git -C "$copy" rev-list --count HEAD)" -eq 1 ]

  action sync '{}'
  [ "$(changes)" -eq 0 ]
  grep -q '"type":"result"' "$MSGS"
}

@test "sync: a new commit is fetched, a source that is gone is removed" {
  action sync '{}'
  copy=$(echo "$CACHE"/*/)
  printf 'more\n' >"$REPO/node-app/EXTRA.md"
  git -C "$REPO" add .
  git -C "$REPO" commit --quiet -m two

  action sync '{}' 1
  [ "$(changes)" -eq 1 ]
  [ ! -e "${copy}node-app/EXTRA.md" ]
  action sync '{}'
  [ "$(changes)" -eq 1 ]
  [ -f "${copy}node-app/EXTRA.md" ]

  mkdir "$CACHE/keep"
  SOURCES='[]'
  action sync '{}' 1
  [ -d "$copy" ]
  action sync '{}'
  grep -q '"op":"delete"' "$MSGS"
  [ ! -e "$copy" ]
  [ -d "$CACHE/keep" ]
}

@test "sync: an unreachable source is reported and ends with exit code 3" {
  SOURCES="[\"git+file://$BATS_TEST_TMPDIR/nowhere\"]"
  run -3 action sync '{}'
  grep -q '"level":"warn"' "$MSGS"
  run ! grep -q '"type":"result"' "$MSGS"
}

@test "sync: an address that is not a Git source is refused with exit code 2" {
  SOURCES='["https://git.example.com/templates.git"]'
  run -2 action sync '{}'
  [ ! -e "$CACHE" ]
}

@test "available: missing before the first sync, then both templates with id, name and language" {
  run available
  [ "$status" -eq 0 ]
  [ "$(jq -r '.status' <<<"$output")" = missing ]
  [ "$(jq -r '.hints[0].action' <<<"$output")" = templates.sync ]

  action sync '{}'
  run available
  [ "$status" -eq 0 ]
  [ "$(jq -c '[.data.items[] | [.id, .name, .language]]' <<<"$output")" = '[["node-app","Node application","node"],["java-service","Java service","java"]]' ]
  [ "$(jq -r '.data.items[1].hooks' <<<"$output")" -eq 2 ]
  [ "$(jq -c '.data.items[0].params' <<<"$output")" = '["projectName","port","license"]' ]
  # the folder with the invalid template.json is reported, not offered
  [ "$(jq -r '.status' <<<"$output")" = warn ]
  [ "$(jq -r '.summary' <<<"$output")" = '2 templates (java, node)' ]
  [[ "$(jq -r '.details[] | select(.label == "Invalid: broken") | .value' <<<"$output")" == *"unknown key extra"*"id is missing or not a valid ID"* ]]
}

@test "new: files are copied, placeholders replaced only in the files marked for rendering" {
  action sync '{}'
  action new '{ "template": "node-app", "name": "shop", "values": { "projectName": "shop-api", "port": 8080 } }'
  [ "$(tree "$ROOT/shop")" = "$(printf '644 ./README.md\n755 ./bin/run.sh\n644 ./package.json\n644 ./src/index.js')" ]
  [ "$(jq -c '.' "$ROOT/shop/package.json")" = '{"name":"shop-api","license":"MIT","config":{"port":8080}}' ]
  printf '# shop-api\n\nListens on 8080. {{unknown}} & {{ port }} stay.\n\n' | cmp - "$ROOT/shop/README.md"
  grep -qx 'exec node src/index.js --port 8080' "$ROOT/shop/bin/run.sh"
  cmp "$ROOT/shop/src/index.js" "$REPO/node-app/src/index.js"
  [ "$(changes)" -eq 5 ]
  grep -q '"type":"result"' "$MSGS"
}

@test "new: include limits the files, the commands of the template are listed and not run" {
  action sync '{}'
  action new '{ "template": "java-service", "name": "svc", "values": {} }'
  [ "$(tree "$ROOT/svc")" = "$(printf '644 ./pom.xml\n644 ./src/main/java/App.java')" ]
  grep -qx '<project><groupId>com.example</groupId></project>' "$ROOT/svc/pom.xml"
  grep -q "not run here: 'touch' 'hooked one.txt'" "$MSGS"
  [ ! -e "$ROOT/svc/hooked one.txt" ]
}

@test "new: a value is inserted as it is, without a second replacement" {
  action sync '{}'
  action new '{ "template": "java-service", "name": "odd", "values": { "groupId": "a&b\\1 \"q\" {{groupId}} $HOME" } }'
  grep -qxF '<project><groupId>a&b\1 "q" {{groupId}} $HOME</groupId></project>' "$ROOT/odd/pom.xml"
}

@test "new: dry run writes nothing and lists every file as a change" {
  action sync '{}'
  action new '{ "template": "node-app", "name": "shop", "values": { "projectName": "shop-api" } }' 1
  [ ! -e "$ROOT" ]
  [ "$(changes)" -eq 5 ]
  for f in package.json README.md src/index.js bin/run.sh; do
    grep -q "\"op\":\"create\".*\"target\":\"$ROOT/shop/$f\"" "$MSGS"
  done
  run ! grep -q 'gitkeep\|template.json' "$MSGS"
}

@test "new: an existing folder with content is refused, an empty one is used" {
  action sync '{}'
  mkdir -p "$ROOT/shop" "$ROOT/empty"
  printf 'mine\n' >"$ROOT/shop/notes.txt"
  run -2 action new '{ "template": "node-app", "name": "shop", "values": { "projectName": "shop-api" } }'
  [ "$(tree "$ROOT/shop")" = '644 ./notes.txt' ]
  grep -q 'exists and is not empty' "$MSGS"
  run -2 action new '{ "template": "node-app", "name": "shop", "values": { "projectName": "shop-api" } }' 1

  action new '{ "template": "node-app", "name": "empty", "values": { "projectName": "shop-api" } }'
  [ -f "$ROOT/empty/package.json" ]
  # a second run finds the folder filled and refuses
  run -2 action new '{ "template": "node-app", "name": "empty", "values": { "projectName": "shop-api" } }'
}

@test "new: wrong names, templates and values end with exit code 2 and write nothing" {
  action sync '{}'
  run -2 action new '{ "template": "node-app", "name": "../out", "values": { "projectName": "a" } }'
  run -2 action new '{ "template": "nothing", "name": "x", "values": {} }'
  run -2 action new '{ "template": "broken", "name": "x", "values": {} }'
  run -2 action new '{ "template": "node-app", "name": "x", "values": {} }'
  grep -q 'projectName needs a value' "$MSGS"
  run -2 action new '{ "template": "node-app", "name": "x", "values": { "projectName": "Bad Name" } }'
  grep -q 'projectName does not match' "$MSGS"
  run -2 action new '{ "template": "node-app", "name": "x", "values": { "projectName": "a", "port": "80" } }'
  grep -q 'port is not of type integer' "$MSGS"
  run -2 action new '{ "template": "node-app", "name": "x", "values": { "projectName": "a", "license": "GPL" } }'
  run -2 action new '{ "template": "node-app", "name": "x", "values": { "projectName": "a", "other": 1 } }'
  grep -q 'other is not a parameter of the template' "$MSGS"
  [ ! -e "$ROOT" ]
}

@test "hooks: the dry run lists the commands, the run executes them once" {
  action sync '{}'
  action new '{ "template": "java-service", "name": "svc", "values": {} }'
  action hooks '{ "template": "java-service", "name": "svc", "again": false }' 1
  [ "$(changes)" -eq 2 ]
  grep -q "run: 'touch' 'hooked one.txt'" "$MSGS"
  [ ! -e "$ROOT/svc/hooked one.txt" ]

  action hooks '{ "template": "java-service", "name": "svc", "again": false }'
  [ -f "$ROOT/svc/hooked one.txt" ]
  [ -f "$ROOT/svc/second.txt" ]
  [ "$(changes)" -eq 2 ]

  rm "$ROOT/svc/second.txt"
  action hooks '{ "template": "java-service", "name": "svc", "again": false }'
  [ "$(changes)" -eq 0 ]
  [ ! -e "$ROOT/svc/second.txt" ]
  action hooks '{ "template": "java-service", "name": "svc", "again": true }'
  [ -f "$ROOT/svc/second.txt" ]

  run -2 action hooks '{ "template": "java-service", "name": "gone", "again": false }'
}
