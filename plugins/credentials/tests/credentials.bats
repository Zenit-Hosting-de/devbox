#!/usr/bin/env bats
# Tests of the credentials plugin: the expiry check against credentials directories
# written for each case.

setup() {
  dir=$(mktemp -d)
}

teardown() {
  rm -rf "$dir"
}

# write_file EXPIRES…: a credentials directory with one instance per expiry date
# (date -d syntax, or "none" for an instance without expiry).
write_file() {
  local i=0 at entries=""
  for at in "$@"; do
    i=$((i + 1))
    if [ "$at" = none ]; then
      entries+="${entries:+,}\"cred-$i\":{\"type\":\"registry\",\"origin\":\"user\",\"secret\":{\"ref\":\"age:cred-$i\"}}"
    else
      entries+="${entries:+,}\"cred-$i\":{\"type\":\"registry\",\"origin\":\"user\",\"secret\":{\"ref\":\"age:cred-$i\",\"expiresAt\":\"$(date -u -d "$at" +%Y-%m-%dT%H:%M:%SZ)\"}}"
    fi
  done
  printf '{"instances":{%s}}\n' "$entries" >"$dir/credentials.json"
}

expiry() {
  devbox plugin test --checks-only --set "credentials.file=$dir/credentials.json" "$@" "$BATS_TEST_DIRNAME/.."
}

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "no credentials directory is ok" {
  run expiry
  [ "$status" -eq 0 ]
  [[ "$output" == *"check"*"credentials.expiry"*"ok"* ]]
}

@test "far expiry and no expiry are ok" {
  write_file "+400 days" none
  run expiry
  [ "$status" -eq 0 ]
  [[ "$output" == *"credentials.expiry"*"ok"* ]]
}

@test "expiry within warnDays warns" {
  write_file "+3 days" "+400 days"
  run expiry
  [[ "$output" == *"credentials.expiry"*"warn"* ]]
  run expiry --set credentials.warnDays=2
  [[ "$output" == *"credentials.expiry"*"ok"* ]]
}

@test "an expired secret is an error" {
  write_file "-1 days" "+3 days"
  run expiry
  [[ "$output" == *"credentials.expiry"*"error"* ]]
}
