#!/usr/bin/env bats
# Offline tests of the pip plugin. The consumer flows (file and env mode, remove,
# dry run, pip and uv against the fake registry) run in test/testscript/pip.txtar.

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$BATS_TEST_DIRNAME/.."
}

@test "the check runs and reports the default index as ok, an unreadable file as warn" {
  conf="$BATS_TEST_TMPDIR/pip.conf"
  HOME="$BATS_TEST_TMPDIR" run devbox plugin test --apply --set pip.pipConfig="$conf" "$BATS_TEST_DIRNAME/.."
  [ "$status" -eq 0 ]
  [[ "$output" == *"check"*"pip.config"*"ok"* ]]
  [[ "$output" != *"failed"* ]]
  [ ! -e "$conf" ]
  [ "$(id -u)" -ne 0 ] || skip "root reads every file"
  : >"$conf"
  chmod 000 "$conf"
  HOME="$BATS_TEST_TMPDIR" run devbox plugin test --set pip.pipConfig="$conf" "$BATS_TEST_DIRNAME/.."
  chmod 600 "$conf"
  [[ "$output" == *"check"*"pip.config"*"warn"* ]]
}

@test "index addresses follow the layout" {
  # shellcheck source=../lib/layout.sh
  . "$BATS_TEST_DIRNAME/../lib/layout.sh"
  run index_url nexus https://nexus.example.com/ pypi-group ""
  [ "$output" = "https://nexus.example.com/repository/pypi-group/simple" ]
  run index_url artifactory https://art.example.com pypi-virtual ""
  [ "$output" = "https://art.example.com/artifactory/api/pypi/pypi-virtual/simple" ]
  run index_url gitea https://gitea.example.com acme ""
  [ "$output" = "https://gitea.example.com/api/packages/acme/pypi/simple" ]
  run index_url gitlab https://gitlab.example.com 42 ""
  [ "$output" = "https://gitlab.example.com/api/v4/projects/42/packages/pypi/simple" ]
  run index_url custom https://x.example.com "" https://pypi.example.com/simple/
  [ "$output" = "https://pypi.example.com/simple" ]
  run index_url nexus https://x.example.com pypi-group https://pypi.example.com/other
  [ "$output" = "https://pypi.example.com/other" ]
}

@test "a layout without enough options is refused" {
  # shellcheck source=../lib/layout.sh
  . "$BATS_TEST_DIRNAME/../lib/layout.sh"
  run index_url custom https://x.example.com "" ""
  [ "$status" -eq 2 ]
  run index_url nexus https://x.example.com "" ""
  [ "$status" -eq 2 ]
  run index_url gitlab https://x.example.com group/name ""
  [ "$status" -eq 2 ]
}

@test "hosts, credentials in addresses and quoting" {
  # shellcheck source=../lib/layout.sh
  . "$BATS_TEST_DIRNAME/../lib/layout.sh"
  run url_host https://nexus.example.com:8443/repository/pypi-group/simple
  [ "$output" = "nexus.example.com" ]
  run url_host https://u:p@pypi.example.com/simple
  [ "$output" = "pypi.example.com" ]
  run pct_encode 'a:b@c/d e%ü'
  [ "$output" = "a%3Ab%40c%2Fd%20e%25%C3%BC" ]
  run url_with_auth https://pypi.example.com/simple alice 's:@cret'
  [ "$output" = "https://alice:s%3A%40cret@pypi.example.com/simple" ]
  run strip_userinfo "https://alice:s%3A%40cret@pypi.example.com/simple"
  [ "$output" = "https://pypi.example.com/simple" ]
  run strip_userinfo "https://pypi.example.com/a@b"
  [ "$output" = "https://pypi.example.com/a@b" ]
  run shell_quote "a'b"
  [ "$output" = "'a'\\''b'" ]
  run shell_unquote "'a'\\''b'"
  [ "$output" = "a'b" ]
  netrc_safe 't0k3n:ok@1'
  run netrc_safe 'two words'
  [ "$status" -ne 0 ]
  run netrc_safe 'a"b'
  [ "$status" -ne 0 ]
  run netrc_safe ''
  [ "$status" -ne 0 ]
}
