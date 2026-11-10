#!/usr/bin/env bats
# Tests of the cleanup plugin. Every test works on fixture trees in a temporary HOME;
# the actions run through devbox run, so settings, dry run and messages take the same
# way as in real use. The flow through the bundled plugin runs in
# test/testscript/cleanup.txtar.

bats_require_minimum_version 1.5.0

setup() {
  PLUGIN="$BATS_TEST_DIRNAME/.."
  export HOME="$BATS_TEST_TMPDIR/home"
  unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME XDG_CACHE_HOME XDG_RUNTIME_DIR
  export DEVBOX_SYSTEM_CONFIG="$BATS_TEST_TMPDIR/etc/config.json"
  ROOT="$HOME/projects"
  ROOTS="cleanup.projectRoots=[\"$ROOT\"]"
  mkdir -p "$HOME/.config/devbox" "$HOME/.cache" "$ROOT"
  # the plugin under test instead of the bundled copy
  printf '{ "plugins": { "paths": ["%s"] } }\n' "$(cd "$PLUGIN/.." && pwd)" >"$HOME/.config/devbox/config.json"
}

# fill DIR [SIZE_KB]: a folder with one file of the given size (default 64 kB).
fill() {
  mkdir -p "$1"
  head -c "$((${2:-64} * 1024))" /dev/zero >"$1/data.bin"
}

# age PATH…: makes everything in the paths 100 days old.
age() {
  find "$@" -depth -exec touch -h -d '100 days ago' {} +
}

# tree: every entry below HOME except DevBox's own folders, with type and link target;
# files also with the time of their last change.
tree() {
  (cd "$HOME" && find . -path ./.local -prune -o -path ./.cache/devbox -prune -o \
    \( -type d -printf 'd %p\n' -o -printf '%y %p %l %T@\n' \) | sort)
}

# refute CMD…: fails the test when the command succeeds (a bare "!" would not).
refute() {
  if "$@"; then
    echo "unexpected success: $*" >&2
    return 1
  fi
}

# caches: fills the six standard cache folders.
caches() {
  fill "$HOME/.npm/_cacache" 128
  fill "$HOME/.m2/repository" 2048
  fill "$HOME/.gradle/caches"
  fill "$HOME/.nuget/packages"
  fill "$HOME/.cache/pip"
  fill "$HOME/.cache/uv"
}

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$PLUGIN"
}

@test "plugin test: check and dry runs pass in an empty home" {
  run devbox plugin test --apply "$PLUGIN"
  [ "$status" -eq 0 ]
  [[ "$output" == *"cleanup.usage"*"ok"* ]]
  [[ "$output" == *"dry-run"*"cleanup.caches"*"0 planned changes"* ]]
  [[ "$output" == *"dry-run"*"cleanup.projects"*"0 planned changes"* ]]
  [[ "$output" == *"dry-run"*"cleanup.docker"*"0 planned changes"* ]]
  [[ "$output" != *failed* ]]
}

@test "usage: one result with the size of every cache, warning above the threshold" {
  run --separate-stderr devbox status --json
  [ "$status" -eq 0 ]
  [[ "$output" == *'"summary": "No caches found"'* ]]

  caches
  run --separate-stderr devbox status --json
  [ "$status" -eq 0 ]
  [[ "$output" == *'"id": "cleanup.usage"'* ]]
  [[ "$output" =~ \"summary\":\ \"Caches\ use\ 2\.[0-9]\ MB\" ]]

  run --separate-stderr devbox status --json --set cleanup.warnAboveMb=1
  [[ "$output" =~ \"status\":\ \"warn\" ]]
  [[ "$output" == *'more than 1.0 MB'* ]]
}

@test "usage: data and details list every cache, links and unsafe folders are not measured" {
  caches
  rm -rf "$HOME/.cache/uv"
  ln -s "$HOME/.m2" "$HOME/.cache/uv"
  cat >"$BATS_TEST_TMPDIR/request.json" <<JSON
{
  "protocol": 1, "kind": "check", "plugin": "cleanup", "name": "usage", "dryRun": false,
  "host": { "distro": "debian", "distroVersion": "12", "arch": "amd64" },
  "settings": {
    "caches": ["npm", "maven", "nuget", "uv"], "warnAboveMb": 0,
    "npmCache": "$HOME/.npm/_cacache", "mavenCache": "$HOME/.m2/repository/",
    "nugetCache": "", "uvCache": "$HOME/.cache/uv"
  }
}
JSON
  check() {
    DEVBOX_BIN=$(command -v devbox) DEVBOX_PLUGIN_DIR="$PLUGIN" DEVBOX_REQUEST="$BATS_TEST_TMPDIR/request.json" \
      "$PLUGIN/checks/usage.sh"
  }
  run --separate-stderr check
  [ "$status" -eq 0 ]
  [ "$(wc -l <<<"$output")" -eq 1 ]
  [ "$(devbox sdk json-get - /status <<<"$output")" = ok ]
  [ "$(devbox sdk json-get - '/data/items/*/name' <<<"$output" | tr '\n' ' ')" = "npm maven " ]
  [ "$(devbox sdk json-get - /data/items/1/path <<<"$output")" = "$HOME/.m2/repository" ]
  [ "$(devbox sdk json-get - /data/totalBytes <<<"$output")" -ge $((2176 * 1024)) ]
  [ "$(devbox sdk json-get - /details/1/label <<<"$output")" = "Maven local repository" ]
  [ "$(devbox sdk json-get - /details/1/value <<<"$output")" = "2.0 MB" ]

  # above the threshold: a warning with the action as hint
  sed -i 's|"warnAboveMb": 0|"warnAboveMb": 1|' "$BATS_TEST_TMPDIR/request.json"
  run --separate-stderr check
  [ "$(devbox sdk json-get - /status <<<"$output")" = warn ]
  [ "$(devbox sdk json-get - /hints/0/action <<<"$output")" = cleanup.caches ]

  sed -i "s|\"npmCache\": \"[^\"]*\"|\"npmCache\": \"$HOME\"|" "$BATS_TEST_TMPDIR/request.json"
  run --separate-stderr check
  [ "$status" -eq 0 ]
  [ "$(devbox sdk json-get - /status <<<"$output")" = error ]
}

@test "caches: the dry run reports each existing cache with its size and deletes nothing" {
  caches
  rm -rf "$HOME/.cache/uv"
  tree >"$BATS_TEST_TMPDIR/before"
  run --separate-stderr devbox run cleanup.caches --dry-run --json
  [ "$status" -eq 0 ]
  tree | cmp - "$BATS_TEST_TMPDIR/before"
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 5 ]
  [[ "$output" =~ \"op\":\"delete\",\"summary\":\"npm\ cache,\ 1[0-9][0-9]\ kB\",\"target\":\"[^\"]*/\.npm/_cacache\" ]]
  [[ "$output" =~ \"summary\":\"Maven\ local\ repository,\ 2\.0\ MB\" ]]
  [[ "$output" =~ \"summary\":\"Would\ free\ 2\.[0-9]\ MB\ in\ 5\ caches\" ]]
  refute grep -q 'uv cache' <<<"$output"
}

@test "caches: the real run deletes the caches, a second run changes nothing" {
  caches
  fill "$HOME/.npm/_logs"
  run --separate-stderr devbox run cleanup.caches --yes --json
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 6 ]
  [[ "$output" =~ \"summary\":\"Freed\ 2\.[0-9]\ MB\ in\ 6\ caches\" ]]
  for d in .npm/_cacache .m2/repository .gradle/caches .nuget/packages .cache/pip .cache/uv; do
    [ ! -e "$HOME/$d" ]
  done
  # what lies next to a cache stays
  [ -f "$HOME/.npm/_logs/data.bin" ]
  [ -d "$HOME/.m2" ]

  run --separate-stderr devbox run cleanup.caches --yes --json
  [ "$status" -eq 0 ]
  refute grep -q '"type":"change"' <<<"$output"
  [[ "$output" == *'"summary":"No caches to clean"'* ]]
}

@test "caches: without confirmation nothing is deleted" {
  caches
  run devbox run cleanup.caches </dev/null
  [ "$status" -eq 4 ]
  [ -f "$HOME/.m2/repository/data.bin" ]
}

@test "caches: only the selected ones, overridden folders, empty settings and links" {
  caches
  fill "$BATS_TEST_TMPDIR/elsewhere/store"
  fill "$BATS_TEST_TMPDIR/elsewhere/linked"
  rm -rf "$HOME/.cache/pip"
  ln -s "$BATS_TEST_TMPDIR/elsewhere/linked" "$HOME/.cache/pip"
  run --separate-stderr devbox run cleanup.caches --yes --json \
    --set 'cleanup.caches=["npm","maven","pip"]' \
    --set "cleanup.npmCache=$BATS_TEST_TMPDIR/elsewhere/store" \
    --set cleanup.mavenCache=
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 1 ]
  [ ! -e "$BATS_TEST_TMPDIR/elsewhere/store" ]
  # the standard folder is not the configured one, the empty setting switches Maven off
  [ -f "$HOME/.npm/_cacache/data.bin" ]
  [ -f "$HOME/.m2/repository/data.bin" ]
  # not selected
  [ -f "$HOME/.gradle/caches/data.bin" ]
  # a symbolic link is neither followed nor removed
  [ -L "$HOME/.cache/pip" ]
  [ -f "$BATS_TEST_TMPDIR/elsewhere/linked/data.bin" ]
  [[ "$output" == *'"level":"warn"'*'symbolic link'* ]]
}

@test "caches: the home directory, folders above it and top-level folders are refused" {
  caches
  for bad in "$HOME" "$HOME/" "$(dirname "$HOME")" / /tmp relative/path "$HOME/a/../.."; do
    run --separate-stderr devbox run cleanup.caches --yes --json --set "cleanup.npmCache=$bad"
    [ "$status" -eq 2 ]
    refute grep -q '"type":"change"' <<<"$output"
  done
  # nothing was removed, not even the caches with valid folders
  [ -f "$HOME/.m2/repository/data.bin" ]
  [ -f "$HOME/.cache/uv/data.bin" ]
}

@test "projects: the dry run lists folder and size and deletes nothing" {
  fill "$ROOT/app/node_modules/left-pad" 512
  fill "$ROOT/app/src"
  fill "$ROOT/svc/target/classes" 2048
  age "$ROOT"
  tree >"$BATS_TEST_TMPDIR/before"
  run --separate-stderr devbox run cleanup.projects --dry-run --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  tree | cmp - "$BATS_TEST_TMPDIR/before"
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 2 ]
  [[ "$output" =~ \"op\":\"delete\",\"summary\":\"build\ folder,\ 5[0-9][0-9]\ kB\",\"target\":\"[^\"]*/projects/app/node_modules\" ]]
  [[ "$output" =~ \"summary\":\"build\ folder,\ 2\.0\ MB\",\"target\":\"[^\"]*/projects/svc/target\" ]]
  [[ "$output" =~ \"summary\":\"Would\ free\ 2\.[0-9]\ MB\ in\ 2\ build\ folders\" ]]
}

@test "projects: the real run deletes only matching, old folders" {
  fill "$ROOT/app/node_modules/left-pad"
  fill "$ROOT/app/src"
  fill "$ROOT/app/docs/build"
  fill "$ROOT/py/pkg/__pycache__"
  fill "$ROOT/py/.venv/lib"
  fill "$ROOT/jvm/.gradle"
  fill "$ROOT/jvm/build/libs"
  fill "$ROOT/web/dist"
  # a file named like a build folder is no folder
  mkdir -p "$ROOT/misc"
  echo text >"$ROOT/misc/target"
  age "$ROOT"
  # recent: one fresh file deep inside keeps the whole folder
  fill "$ROOT/fresh/node_modules/a/b"
  age "$ROOT/fresh"
  touch "$ROOT/fresh/node_modules/a/b/data.bin"
  # outside the project folders
  fill "$HOME/other/node_modules"
  age "$HOME/other"

  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 6 ]
  [[ "$output" =~ \"summary\":\"Freed\ [0-9.]+\ (kB|MB)\ in\ 6\ build\ folders\" ]]
  for d in app/node_modules app/docs/build py/pkg/__pycache__ jvm/.gradle jvm/build web/dist; do
    [ ! -e "$ROOT/$d" ]
  done
  [ -f "$ROOT/app/src/data.bin" ]
  [ -f "$ROOT/py/.venv/lib/data.bin" ]
  [ -f "$ROOT/misc/target" ]
  [ -f "$ROOT/fresh/node_modules/a/b/data.bin" ]
  [ -f "$HOME/other/node_modules/data.bin" ]
  [[ "$output" == *'"keptRecent":1'* ]]

  # idempotent: nothing left to do
  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  refute grep -q '"type":"change"' <<<"$output"
  [[ "$output" == *'"summary":"No build folders to clean"'* ]]
}

@test "projects: olderThanDays decides" {
  fill "$ROOT/app/dist"
  find "$ROOT/app" -depth -exec touch -d '10 days ago' {} +
  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ -d "$ROOT/app/dist" ]
  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS" --set cleanup.olderThanDays=7
  [ "$status" -eq 0 ]
  [ ! -e "$ROOT/app/dist" ]
}

@test "projects: bin and obj only next to a .NET project file, own patterns and markers" {
  fill "$ROOT/net/bin/Debug"
  fill "$ROOT/net/obj"
  touch "$ROOT/net/App.csproj"
  fill "$ROOT/tools/bin"
  fill "$ROOT/tools/obj"
  fill "$ROOT/rust/target"
  fill "$ROOT/other/target"
  touch "$ROOT/rust/Cargo.toml"
  fill "$ROOT/x/out-debug"
  age "$ROOT"

  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS" \
    --set 'cleanup.patterns=["bin:*.*proj","obj:*.*proj"]'
  [ "$status" -eq 0 ]
  [ ! -e "$ROOT/net/bin" ]
  [ ! -e "$ROOT/net/obj" ]
  [ -f "$ROOT/net/App.csproj" ]
  [ -f "$ROOT/tools/bin/data.bin" ]
  [ -f "$ROOT/tools/obj/data.bin" ]
  [ -f "$ROOT/rust/target/data.bin" ]

  # several markers for one name, and a wildcard name
  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS" \
    --set 'cleanup.patterns=["target:pom.xml","target:Cargo.toml","out-*"]'
  [ "$status" -eq 0 ]
  [ ! -e "$ROOT/rust/target" ]
  [ ! -e "$ROOT/x/out-debug" ]
  [ -f "$ROOT/other/target/data.bin" ]
}

@test "projects: symbolic links are neither followed nor removed" {
  fill "$BATS_TEST_TMPDIR/outside/node_modules/pkg"
  fill "$BATS_TEST_TMPDIR/outside/keep"
  age "$BATS_TEST_TMPDIR/outside"
  mkdir -p "$ROOT/app"
  # a link named like a build folder, a link to a folder that holds one, and a link
  # inside a build folder that points outside
  ln -s "$BATS_TEST_TMPDIR/outside/node_modules" "$ROOT/app/node_modules"
  ln -s "$BATS_TEST_TMPDIR/outside" "$ROOT/linked"
  fill "$ROOT/app/build"
  ln -s "$BATS_TEST_TMPDIR/outside/keep" "$ROOT/app/build/keep"
  age "$ROOT"

  run --separate-stderr devbox run cleanup.projects --dry-run --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 1 ]
  [[ "$output" == *'/projects/app/build"'* ]]

  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 1 ]
  [ ! -e "$ROOT/app/build" ]
  [ -L "$ROOT/app/node_modules" ]
  [ -L "$ROOT/linked" ]
  [ -f "$BATS_TEST_TMPDIR/outside/node_modules/pkg/data.bin" ]
  [ -f "$BATS_TEST_TMPDIR/outside/keep/data.bin" ]
}

@test "projects: a project folder that is a link is resolved once, the search stays below it" {
  fill "$BATS_TEST_TMPDIR/disk/work/app/dist"
  fill "$BATS_TEST_TMPDIR/disk/dist"
  age "$BATS_TEST_TMPDIR/disk"
  rmdir "$ROOT"
  ln -s "$BATS_TEST_TMPDIR/disk/work" "$ROOT"
  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ ! -e "$BATS_TEST_TMPDIR/disk/work/app/dist" ]
  [ -f "$BATS_TEST_TMPDIR/disk/dist/data.bin" ]
  [ -L "$ROOT" ]
}

@test "projects: folders tracked by Git and Git repositories stay" {
  command -v git >/dev/null || skip "git is not installed"
  repo="$ROOT/repo"
  fill "$repo/dist"
  fill "$repo/build"
  fill "$repo/sub/target"
  fill "$repo/node_modules/pkg"
  fill "$repo/vendor/bin"
  touch "$repo/vendor/Tool.csproj"
  git -C "$ROOT" init -q repo
  printf 'node_modules/\n' >"$repo/.gitignore"
  # dist and vendor/bin are checked in, sub/target partly; build and node_modules not
  git -C "$repo" add .gitignore dist sub/target/data.bin vendor
  echo more >"$repo/sub/target/untracked.txt"
  # a build folder that is a repository of its own
  fill "$ROOT/plain/build"
  git -C "$ROOT/plain" init -q build
  age "$ROOT"

  run --separate-stderr devbox run cleanup.projects --dry-run --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 2 ]
  [[ "$output" == *'"keptTracked":4'* ]]

  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 2 ]
  [ ! -e "$repo/build" ]
  [ ! -e "$repo/node_modules" ]
  [ -f "$repo/dist/data.bin" ]
  [ -f "$repo/sub/target/data.bin" ]
  [ -f "$repo/sub/target/untracked.txt" ]
  [ -f "$repo/vendor/bin/data.bin" ]
  [ -d "$ROOT/plain/build/.git" ]
  [ -d "$repo/.git" ]
  git -C "$repo" status --porcelain >/dev/null
}

@test "projects: in a work tree nothing is removed when Git cannot answer" {
  command -v git >/dev/null || skip "git is not installed"
  fill "$ROOT/repo/build"
  git -C "$ROOT" init -q repo
  age "$ROOT"
  # a broken repository: Git fails, so the folder may be tracked
  echo broken >"$ROOT/repo/.git/HEAD"
  run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS"
  [ "$status" -eq 0 ]
  [ -f "$ROOT/repo/build/data.bin" ]
  [[ "$output" == *'"level":"warn"'*'Git could not tell'* ]]
}

@test "projects: unsafe project folders and patterns are refused" {
  fill "$HOME/build"
  age "$HOME/build"
  for bad in "$HOME" "$(dirname "$HOME")" / relative; do
    run --separate-stderr devbox run cleanup.projects --yes --json --set "cleanup.projectRoots=[\"$bad\"]"
    [ "$status" -eq 2 ]
  done
  [ -f "$HOME/build/data.bin" ]
  # a project folder that does not exist is skipped
  run --separate-stderr devbox run cleanup.projects --yes --json --set "cleanup.projectRoots=[\"$HOME/none\"]"
  [ "$status" -eq 0 ]
  for bad in '..' '.git' 'a/b' 'bin:'; do
    run --separate-stderr devbox run cleanup.projects --yes --json --set "$ROOTS" --set "cleanup.patterns=[\"$bad\"]"
    [ "$status" -ne 0 ]
  done
}

# fake_docker: a docker command in PATH that records its calls next to its folder (the
# environment of the test does not reach an action) and answers system df and system
# prune.
fake_docker() {
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  cat >"$BATS_TEST_TMPDIR/bin/docker" <<'SH'
#!/usr/bin/env bash
DOCKER_LOG="$(dirname "$0")/../docker.log"
echo "$*" >>"$DOCKER_LOG"
case "$1 $2" in
"system df")
  if [ -e "$DOCKER_LOG.pruned" ]; then
    printf '%s\n' '{"Active":"0","Reclaimable":"0B (0%)","Size":"0B","TotalCount":"0","Type":"Images"}'
    exit 0
  fi
  printf '%s\n' \
    '{"Active":"1","Reclaimable":"1.2GB (60%)","Size":"2GB","TotalCount":"4","Type":"Images"}' \
    '{"Active":"0","Reclaimable":"0B (0%)","Size":"0B","TotalCount":"0","Type":"Containers"}' \
    '{"Active":"0","Reclaimable":"300MB (100%)","Size":"300MB","TotalCount":"2","Type":"Local Volumes"}' \
    '{"Active":"0","Reclaimable":"512MB","Size":"512MB","TotalCount":"9","Type":"Build Cache"}'
  ;;
"system prune")
  if [ -e "$DOCKER_LOG.pruned" ]; then
    echo "Total reclaimed space: 0B"
  else
    touch "$DOCKER_LOG.pruned"
    printf 'Deleted Images:\ndeleted: sha256:0123\n\nTotal reclaimed space: 1.7GB\n'
  fi
  ;;
*) exit 1 ;;
esac
SH
  chmod 755 "$BATS_TEST_TMPDIR/bin/docker"
  DOCKER_LOG="$BATS_TEST_TMPDIR/docker.log"
  : >"$DOCKER_LOG"
  PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}

@test "docker: switched off by default, docker is not called" {
  fake_docker
  run --separate-stderr devbox run cleanup.docker --yes --json
  [ "$status" -eq 0 ]
  [[ "$output" == *'"summary":"Docker cleanup is switched off"'* ]]
  [ ! -s "$DOCKER_LOG" ]
}

@test "docker: the dry run reports the reclaimable space and prunes nothing" {
  fake_docker
  run --separate-stderr devbox run cleanup.docker --dry-run --json --set cleanup.docker=true
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 2 ]
  [[ "$output" == *'"summary":"up to 1.2GB (60%) reclaimable","target":"docker: Images"'* ]]
  [[ "$output" == *'"summary":"up to 512MB reclaimable","target":"docker: Build Cache"'* ]]
  refute grep -q 'Local Volumes' <<<"$output"
  [ "$(cat "$DOCKER_LOG")" = "system df --format {{json .}}" ]

  run --separate-stderr devbox run cleanup.docker --dry-run --json --set cleanup.docker=true --set cleanup.dockerVolumes=true
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 3 ]
  [[ "$output" == *'"target":"docker: Local Volumes"'* ]]
  refute grep -q prune "$DOCKER_LOG"
}

@test "docker: the real run prunes with the chosen variant, a second run changes nothing" {
  fake_docker
  run --separate-stderr devbox run cleanup.docker --yes --json --set cleanup.docker=true \
    --set cleanup.dockerAllImages=true --set cleanup.dockerVolumes=true
  [ "$status" -eq 0 ]
  [ "$(grep -c '"type":"change"' <<<"$output")" -eq 1 ]
  [[ "$output" == *'"summary":"Docker: 1.7GB reclaimed"'* ]]
  [ "$(cat "$DOCKER_LOG")" = "system prune --force --all --volumes" ]

  run --separate-stderr devbox run cleanup.docker --yes --json --set cleanup.docker=true
  [ "$status" -eq 0 ]
  refute grep -q '"type":"change"' <<<"$output"
  [ "$(tail -n 1 "$DOCKER_LOG")" = "system prune --force" ]
}

@test "docker: a daemon that does not answer is an error" {
  fake_docker
  printf '#!/usr/bin/env bash\necho "Cannot connect to the Docker daemon" >&2\nexit 1\n' >"$BATS_TEST_TMPDIR/bin/docker"
  run --separate-stderr devbox run cleanup.docker --dry-run --json --set cleanup.docker=true
  [ "$status" -eq 1 ]
  run --separate-stderr devbox run cleanup.docker --yes --json --set cleanup.docker=true
  [ "$status" -eq 1 ]
}
