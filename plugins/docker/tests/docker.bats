#!/usr/bin/env bats
# Tests of the docker plugin without network and without root: manifest, checks, the
# pure helpers, and the registry consumer and the configure action run directly with
# a request file in a temporary HOME. Package installation, group, update and the
# flows through the CLI run against fakes in test/testscript/docker.txtar.

PLUGIN="$BATS_TEST_DIRNAME/.."

setup() {
  export HOME="$BATS_TEST_TMPDIR/home"
  mkdir -p "$HOME" "$BATS_TEST_TMPDIR/state" "$BATS_TEST_TMPDIR/cache"
  MSGS="$BATS_TEST_TMPDIR/messages.jsonl"
  ARGS="$BATS_TEST_TMPDIR/arguments.log"
  CFG="$HOME/.docker/config.json"
  DAEMON="$BATS_TEST_TMPDIR/etc/docker/daemon.json"
  : >"$MSGS"
  : >"$ARGS"
  # devbox through a wrapper that records the arguments of every call
  WRAPPER="$BATS_TEST_TMPDIR/devbox"
  printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"%s"\nexec "%s" "$@"\n' "$ARGS" "$(command -v devbox)" >"$WRAPPER"
  chmod 755 "$WRAPPER"
}

# request KIND NAME DRYRUN [CREDENTIAL_JSON]: writes a request file like the runner does.
request() {
  local cred=${4:-null} dry=false
  [ "$3" != 1 ] || dry=true
  REQ="$BATS_TEST_TMPDIR/request.json"
  cat >"$REQ" <<EOF
{
  "protocol": 1, "kind": "$1", "plugin": "docker", "name": "$2", "dryRun": $dry,
  "settings": {
    "registryMirrors": ${S_MIRRORS:-[]}, "logDriver": "${S_LOG_DRIVER:-}",
    "logOptions": ${S_LOG_OPTIONS:-{\}}, "defaultAddressPools": [],
    "serviceReload": "none", "service": "docker",
    "daemonConfig": "$DAEMON", "clientConfig": "$CFG",
    "tokenUsername": "${S_TOKEN_USER:-}"
  },
  "credential": $cred,
  "host": { "distro": "ubuntu", "distroVersion": "24.04", "arch": "amd64" }
}
EOF
}

# cred HOST_OPTION USER SECRET: the credential part of a consumer request.
cred() {
  local options='{}'
  [ -z "$1" ] || options="{\"host\":\"$1\"}"
  printf '{"instance":"corp","type":"registry","fields":{"url":"https://nexus.example.com:8443/repository/docker","layout":"nexus","username":"%s"},"options":%s,"secret":"%s"}' "$2" "$options" "$3"
}

# plugin SCRIPT [DRYRUN]: runs a script of the plugin with the protocol environment.
plugin() {
  DEVBOX_BIN="$WRAPPER" DEVBOX_PLUGIN_DIR="$PLUGIN" DEVBOX_REQUEST="$REQ" \
    DEVBOX_MSG_FD=3 DEVBOX_DRY_RUN="${2:-0}" DEVBOX_BACKUP_DIR="$BATS_TEST_TMPDIR/backup" \
    DEVBOX_STATE_DIR="$BATS_TEST_TMPDIR/state" DEVBOX_CACHE_DIR="$BATS_TEST_TMPDIR/cache" \
    "$PLUGIN/$1" 3>>"$MSGS"
}

changes() { grep -c '"type":"change"' "$MSGS" || true; }

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$PLUGIN"
}

@test "the checks return valid results on this machine" {
  run devbox plugin test --checks-only "$PLUGIN"
  echo "$output"
  [ "$status" -eq 0 ]
  [[ "$output" == *"check"*"docker.installed"* ]]
  [[ "$output" == *"check"*"docker.config"* ]]
  [[ "$output" != *"failed"* ]]
}

@test "hosts, pointer tokens and JSON strings" {
  . "$PLUGIN/lib/common.sh"
  run url_host https://nexus.example.com:8443/repository/docker
  [ "$output" = "nexus.example.com:8443" ]
  run url_host https://user@docker.example.com
  [ "$output" = "docker.example.com" ]
  valid_host docker.example.com:5000
  run valid_host ""
  [ "$status" -ne 0 ]
  run valid_host "docker.example.com/path"
  [ "$status" -ne 0 ]
  run valid_host 'docker.example.com"'
  [ "$status" -ne 0 ]
  run valid_host "docker example.com"
  [ "$status" -ne 0 ]
  run pointer_token 'a~b/c'
  [ "$output" = 'a~0b~1c' ]
  run json_str $'x"y\\z\n'
  [ "$output" = '"x\"y\\z\n"' ]
  run json_list a 'b"c'
  [ "$output" = '["a","b\"c"]' ]
}

@test "the consumer writes the login with mode 0600 and never passes the secret as an argument" {
  request apply registry 0 "$(cred docker.example.com alice 's3cr3t value')"
  plugin consume/apply.sh
  auth=$(printf '%s' 'alice:s3cr3t value' | base64 -w0)
  grep -q "\"auth\": \"$auth\"" "$CFG"
  [ "$(stat -c %a "$CFG")" = 600 ]
  [ "$(changes)" -eq 1 ]
  run grep -q 's3cr3t' "$ARGS" "$MSGS"
  [ "$status" -eq 1 ]
  run grep -q "$auth" "$ARGS" "$MSGS"
  [ "$status" -eq 1 ]
  [ -s "$ARGS" ]

  # second run: no change
  : >"$MSGS"
  plugin consume/apply.sh
  [ "$(changes)" -eq 0 ]
}

@test "a dry run of the consumer changes nothing" {
  request apply registry 1 "$(cred docker.example.com alice s3cr3t)"
  plugin consume/apply.sh 1
  [ "$(changes)" -eq 1 ]
  [ ! -e "$HOME/.docker" ]
  [ ! -e "$BATS_TEST_TMPDIR/state/logins.json" ]
}

@test "a changed host moves the login, removing leaves the file as before" {
  mkdir -p "$HOME/.docker"
  printf '{\n\t"auths": {\n\t\t"ghcr.example.com": {\n\t\t\t"auth": "b3RoZXI6c2VjcmV0"\n\t\t}\n\t},\n\t"credsStore": ""\n}' >"$CFG"
  chmod 644 "$CFG"
  cp "$CFG" "$BATS_TEST_TMPDIR/before.json"

  request apply registry 0 "$(cred docker.example.com alice s3cr3t)"
  plugin consume/apply.sh
  grep -q '"docker.example.com"' "$CFG"
  [ "$(stat -c %a "$CFG")" = 600 ]

  # without the option the host (and port) of the address is used
  request apply registry 0 "$(cred "" alice s3cr3t)"
  plugin consume/apply.sh
  run grep -q '"docker.example.com"' "$CFG"
  [ "$status" -eq 1 ]
  grep -q '"nexus.example.com:8443"' "$CFG"
  grep -q '"ghcr.example.com"' "$CFG"

  request remove registry 0 "$(cred "" alice s3cr3t)"
  plugin consume/remove.sh
  cmp "$CFG" "$BATS_TEST_TMPDIR/before.json"

  # removing again finds nothing to do
  : >"$MSGS"
  plugin consume/remove.sh
  [ "$(changes)" -eq 0 ]
  cmp "$CFG" "$BATS_TEST_TMPDIR/before.json"
}

@test "a login that DevBox did not write and invalid input are refused" {
  mkdir -p "$HOME/.docker"
  printf '{"auths": {"docker.example.com": {"auth": "b3RoZXI6c2VjcmV0"}}}\n' >"$CFG"
  cp "$CFG" "$BATS_TEST_TMPDIR/before.json"
  request apply registry 0 "$(cred docker.example.com alice s3cr3t)"
  run plugin consume/apply.sh
  [ "$status" -eq 1 ]
  [[ "$output" == *"already has a login for docker.example.com"* ]]
  cmp "$CFG" "$BATS_TEST_TMPDIR/before.json"

  request apply registry 0 "$(cred other.example.com "" s3cr3t)"
  run plugin consume/apply.sh
  [ "$status" -eq 2 ]
  request apply registry 0 "$(cred other.example.com a:b s3cr3t)"
  run plugin consume/apply.sh
  [ "$status" -eq 2 ]
  request apply registry 0 "$(cred 'bad host' alice s3cr3t)"
  run plugin consume/apply.sh
  [ "$status" -eq 2 ]
  cmp "$CFG" "$BATS_TEST_TMPDIR/before.json"

  printf 'not json' >"$CFG"
  request apply registry 0 "$(cred other.example.com alice s3cr3t)"
  run plugin consume/apply.sh
  [ "$status" -eq 1 ]
  [ "$(cat "$CFG")" = "not json" ]
}

@test "configure: a dry run changes nothing, a second run changes nothing, emptied settings restore the file" {
  mkdir -p "$(dirname "$DAEMON")"
  printf '{\n  "log-driver": "journald",\n  "storage-driver": "overlay2"\n}\n' >"$DAEMON"
  cp "$DAEMON" "$BATS_TEST_TMPDIR/before.json"

  S_MIRRORS='["https://mirror.example.com"]' S_LOG_DRIVER=local S_LOG_OPTIONS='{"max-size":"10m"}' request action configure 1
  plugin actions/configure.sh 1
  [ "$(changes)" -eq 1 ]
  cmp "$DAEMON" "$BATS_TEST_TMPDIR/before.json"
  [ ! -e "$BATS_TEST_TMPDIR/state/daemon.json" ]

  : >"$MSGS"
  S_MIRRORS='["https://mirror.example.com"]' S_LOG_DRIVER=local S_LOG_OPTIONS='{"max-size":"10m"}' request action configure 0
  plugin actions/configure.sh
  [ "$(changes)" -eq 1 ]
  grep -q '"log-driver": "local"' "$DAEMON"
  grep -q '"storage-driver": "overlay2"' "$DAEMON"
  grep -q '"https://mirror.example.com"' "$DAEMON"
  grep -q '"max-size": "10m"' "$DAEMON"
  cmp "$BATS_TEST_TMPDIR/backup$DAEMON" "$BATS_TEST_TMPDIR/before.json"

  : >"$MSGS"
  plugin actions/configure.sh
  [ "$(changes)" -eq 0 ]

  request action configure 0
  plugin actions/configure.sh
  cmp "$DAEMON" "$BATS_TEST_TMPDIR/before.json"
}
