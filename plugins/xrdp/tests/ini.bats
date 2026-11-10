#!/usr/bin/env bats
# Tests of the INI helper (lib/ini.sh) against files modelled on the stock xrdp.ini and
# sesman.ini: sections, comments, the same key name in several sections, CRLF line
# endings and a missing newline at the end. Whatever is not set stays byte for byte.

load helpers

setup() {
  # shellcheck source=../lib/ini.sh
  source "$BATS_TEST_DIRNAME/../lib/ini.sh"
  INI="$BATS_TEST_TMPDIR/xrdp.ini"
  SESMAN="$BATS_TEST_TMPDIR/sesman.ini"
  OUT="$BATS_TEST_TMPDIR/out.ini"
  write_xrdp_ini "$INI"
  write_sesman_ini "$SESMAN"
}

# changed_lines A B: the lines that differ, as "<old" and ">new", without positions.
changed_lines() { diff "$1" "$2" | grep '^[<>]' || true; }

@test "ini_get reads a key from its section only" {
  [ "$(ini_get "$INI" Globals port)" = 3389 ]
  [ "$(ini_get "$INI" Xorg port)" = -1 ]
  [ "$(ini_get "$INI" Xvnc port)" = -1 ]
  [ "$(ini_get "$INI" Globals security_layer)" = negotiate ]
  [ "$(ini_get "$INI" Globals max_bpp)" = 32 ]
  [ "$(ini_get "$SESMAN" Security AllowRootLogin)" = true ]
  [ "$(ini_get "$INI" Logging LogLevel)" = INFO ]
  [ "$(ini_get "$SESMAN" Logging LogFile)" = xrdp-sesman.log ]
}

@test "ini_get compares section and key names without regard to case" {
  [ "$(ini_get "$INI" globals PORT)" = 3389 ]
  [ "$(ini_get "$SESMAN" SECURITY allowrootlogin)" = true ]
}

@test "ini_get keeps values with blanks, commas and equal signs, and empty values" {
  [ "$(ini_get "$INI" Globals ssl_protocols)" = "TLSv1.2, TLSv1.3" ]
  [ "$(ini_get "$INI" Globals ls_title)" = "My Login Title" ]
  run ini_get "$INI" Globals certificate
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
  printf '[A]\nurl=http://example.com/?a=b&c=d\n' >"$OUT"
  [ "$(ini_get "$OUT" A url)" = "http://example.com/?a=b&c=d" ]
}

@test "ini_get takes the last line of a key that occurs several times" {
  [ "$(ini_get "$SESMAN" Xorg param)" = tcp ]
}

@test "ini_get returns 1 for keys that are missing or commented out" {
  run ini_get "$INI" Globals tls_ciphers
  [ "$status" -eq 1 ]
  [ "$output" = "" ]
  run ini_get "$INI" Globals hidelogwindow
  [ "$status" -eq 1 ]
  run ini_get "$INI" Globals no_such_key
  [ "$status" -eq 1 ]
  run ini_get "$INI" NoSuchSection port
  [ "$status" -eq 1 ]
  # the examples in the comments of [Globals] are not values
  run ini_get "$INI" Logging port
  [ "$status" -eq 1 ]
}

@test "ini_get returns 2 for a file that cannot be read and for invalid names" {
  run ini_get "$BATS_TEST_TMPDIR/missing.ini" Globals port
  [ "$status" -eq 2 ]
  run ini_get "$INI" Globals "a=b"
  [ "$status" -eq 2 ]
  run ini_get "$INI" Globals ""
  [ "$status" -eq 2 ]
  run ini_get "$INI" "Glo]bals" port
  [ "$status" -eq 2 ]
}

@test "ini_set prints the file byte for byte when the value is set already" {
  ini_set "$INI" Globals port 3389 >"$OUT"
  cmp "$INI" "$OUT"
  ini_set "$INI" Globals security_layer negotiate >"$OUT"
  cmp "$INI" "$OUT"
  ini_set "$SESMAN" Security AllowRootLogin true >"$OUT"
  cmp "$SESMAN" "$OUT"
  ini_set "$SESMAN" Xorg param tcp >"$OUT"
  # every param line is set, so this one is a change …
  refute cmp -s "$SESMAN" "$OUT"
  # … but a file whose lines all carry the value stays
  ini_set "$OUT" Xorg param tcp >"$BATS_TEST_TMPDIR/again.ini"
  cmp "$OUT" "$BATS_TEST_TMPDIR/again.ini"
}

@test "ini_set does not write to the file it reads" {
  before=$(cksum <"$INI")
  ini_set "$INI" Globals port 3390 >"$OUT"
  [ "$(cksum <"$INI")" = "$before" ]
}

@test "ini_set changes exactly the line of the key in its section" {
  ini_set "$INI" Globals port 3390 >"$OUT"
  run changed_lines "$INI" "$OUT"
  [ "$output" = $'< port=3389\n> port=3390' ]
  [ "$(ini_get "$OUT" Globals port)" = 3390 ]
  [ "$(ini_get "$OUT" Xorg port)" = -1 ]
  [ "$(ini_get "$OUT" Xvnc port)" = -1 ]
  # the examples in the comments stay
  grep -qxF ';   port=3389' "$OUT"
  [ "$(wc -l <"$INI")" -eq "$(wc -l <"$OUT")" ]
}

@test "ini_set leaves the same key in other sections alone" {
  ini_set "$INI" Xorg port 5910 >"$OUT"
  run changed_lines "$INI" "$OUT"
  [ "$output" = $'< port=-1\n> port=5910' ]
  [ "$(ini_get "$OUT" Globals port)" = 3389 ]
  [ "$(ini_get "$OUT" Xorg port)" = 5910 ]
  [ "$(ini_get "$OUT" Xvnc port)" = -1 ]
}

@test "ini_set finds section and key without regard to case and keeps their spelling" {
  ini_set "$SESMAN" security allowrootlogin false >"$OUT"
  run changed_lines "$SESMAN" "$OUT"
  [ "$output" = $'< AllowRootLogin=true\n> AllowRootLogin=false' ]
  grep -qxF '[Security]' "$OUT"
}

@test "ini_set sets every line of a key that occurs several times in the section" {
  printf '[A]\nk=1\nother=x\nk=2\n[B]\nk=3\n' >"$INI"
  ini_set "$INI" A k 9 >"$OUT"
  [ "$(cat "$OUT")" = $'[A]\nk=9\nother=x\nk=9\n[B]\nk=3' ]
}

@test "ini_set adds a missing key after its commented-out line" {
  ini_set "$INI" Globals tls_ciphers HIGH:!aNULL >"$OUT"
  run changed_lines "$INI" "$OUT"
  [ "$output" = '> tls_ciphers=HIGH:!aNULL' ]
  run grep -n -A1 -xF '#tls_ciphers=HIGH' "$OUT"
  [[ "$output" == *$'\n'*'tls_ciphers=HIGH:!aNULL' ]]
  [ "$(ini_get "$OUT" Globals tls_ciphers)" = 'HIGH:!aNULL' ]
}

@test "ini_set adds a missing key at the end of its section, before the blank lines" {
  ini_set "$INI" Globals require_credentials true >"$OUT"
  run changed_lines "$INI" "$OUT"
  [ "$output" = '> require_credentials=true' ]
  run grep -n -B1 -A2 -xF 'require_credentials=true' "$OUT"
  [[ "${lines[0]}" == *'ls_top_window_bg_color=009cb5' ]]
  [[ "${lines[3]}" == *'[Logging]' ]]
  [ "$(ini_get "$OUT" Globals require_credentials)" = true ]
  run ini_get "$OUT" Logging require_credentials
  [ "$status" -eq 1 ]
}

@test "ini_set adds a missing key to the last section at the end of the file" {
  ini_set "$INI" Xvnc xserverbpp 24 >"$OUT"
  # after its commented-out line, not at the very end
  run grep -n -A1 -xF '#xserverbpp=24' "$OUT"
  [[ "${lines[1]}" == *'xserverbpp=24' ]]
  ini_set "$INI" Xvnc chansrvport 7200 >"$OUT"
  [ "$(tail -n 1 "$OUT")" = 'chansrvport=7200' ]
  [ "$(head -n "$(wc -l <"$INI")" "$OUT" | cksum)" = "$(cksum <"$INI")" ]
}

@test "ini_set adds a missing section at the end of the file" {
  ini_set "$INI" NewSection key value >"$OUT"
  [ "$(head -n "$(wc -l <"$INI")" "$OUT" | cksum)" = "$(cksum <"$INI")" ]
  [ "$(tail -n 3 "$OUT")" = $'\n[NewSection]\nkey=value' ]
  [ "$(ini_get "$OUT" NewSection key)" = value ]
}

@test "ini_set fills an empty file and a file without sections" {
  : >"$INI"
  ini_set "$INI" Globals port 3389 >"$OUT"
  [ "$(cat "$OUT")" = $'[Globals]\nport=3389' ]
  printf 'a=1\n\n[S]\nb=2\n' >"$INI"
  ini_set "$INI" "" a 5 >"$OUT"
  [ "$(cat "$OUT")" = $'a=5\n\n[S]\nb=2' ]
  ini_set "$INI" "" c 7 >"$OUT"
  [ "$(cat "$OUT")" = $'a=1\nc=7\n\n[S]\nb=2' ]
  [ "$(ini_get "$OUT" "" c)" = 7 ]
}

@test "ini_set keeps indentation and the spacing around the equal sign" {
  printf '[A]\n  key = old value  \n\tother\t=\tx\n' >"$INI"
  ini_set "$INI" A key new >"$OUT"
  [ "$(cat "$OUT")" = $'[A]\n  key = new\n\tother\t=\tx' ]
  ini_set "$INI" A other y >"$OUT"
  [ "$(sed -n 3p "$OUT")" = $'\tother\t=\ty' ]
  # trailing blanks do not make a value differ
  ini_set "$INI" A key "old value" >"$OUT"
  cmp "$INI" "$OUT"
}

@test "ini_set keeps CRLF line endings" {
  printf '[Globals]\r\n; comment\r\nport=3389\r\n\r\n[Xorg]\r\nport=-1\r\n' >"$INI"
  ini_set "$INI" Globals port 3390 >"$OUT"
  [ "$(cat "$OUT")" = $'[Globals]\r\n; comment\r\nport=3390\r\n\r\n[Xorg]\r\nport=-1\r' ]
  [ "$(ini_get "$OUT" Globals port)" = 3390 ]
  ini_set "$INI" Globals port 3389 >"$OUT"
  cmp "$INI" "$OUT"
  # new lines get the line ending of the file
  ini_set "$INI" Globals max_bpp 24 >"$OUT"
  [ "$(sed -n 4p "$OUT")" = $'max_bpp=24\r' ]
  ini_set "$INI" Added key v >"$OUT"
  [ "$(tail -n 2 "$OUT")" = $'[Added]\r\nkey=v\r' ]
}

@test "ini_set keeps a missing newline at the end of the file" {
  printf '[A]\nk=1\n[B]\nlast=1' >"$INI"
  ini_set "$INI" A k 2 >"$OUT"
  [ "$(cat "$OUT" && printf x)" = $'[A]\nk=2\n[B]\nlast=1x' ]
  ini_set "$INI" B last 2 >"$OUT"
  [ "$(cat "$OUT" && printf x)" = $'[A]\nk=1\n[B]\nlast=2x' ]
  ini_set "$INI" B last 1 >"$OUT"
  cmp "$INI" "$OUT"
  # a line added after the last one ends the file with a newline
  ini_set "$INI" B more 3 >"$OUT"
  [ "$(cat "$OUT" && printf x)" = $'[A]\nk=1\n[B]\nlast=1\nmore=3\nx' ]
  ini_set "$INI" C c 1 >"$OUT"
  [ "$(cat "$OUT" && printf x)" = $'[A]\nk=1\n[B]\nlast=1\n\n[C]\nc=1\nx' ]
}

@test "ini_set writes values literally" {
  ini_set "$INI" Globals ls_title 'a=b [c] ; d \n * $HOME %s' >"$OUT"
  [ "$(ini_get "$OUT" Globals ls_title)" = 'a=b [c] ; d \n * $HOME %s' ]
  run changed_lines "$INI" "$OUT"
  [ "${#lines[@]}" -eq 2 ]
  ini_set "$INI" Globals certificate "" >"$OUT"
  cmp "$INI" "$OUT"
  ini_set "$INI" Globals autorun "" >"$OUT"
  cmp "$INI" "$OUT"
}

@test "ini_set ignores section headers and keys inside comments" {
  printf '; [A]\n# k=0\n[B]\nx=1\n;[A]\n[A]\n;k=1\nk=2\n' >"$INI"
  ini_set "$INI" A k 3 >"$OUT"
  [ "$(cat "$OUT")" = $'; [A]\n# k=0\n[B]\nx=1\n;[A]\n[A]\n;k=1\nk=3' ]
  run ini_get "$INI" B k
  [ "$status" -eq 1 ]
}

@test "ini_set applied twice gives the same file" {
  ini_set "$INI" Globals port 3390 >"$OUT"
  ini_set "$OUT" Globals max_bpp 24 >"$BATS_TEST_TMPDIR/two.ini"
  ini_set "$BATS_TEST_TMPDIR/two.ini" Globals port 3390 >"$BATS_TEST_TMPDIR/three.ini"
  ini_set "$BATS_TEST_TMPDIR/three.ini" Globals max_bpp 24 >"$BATS_TEST_TMPDIR/four.ini"
  cmp "$BATS_TEST_TMPDIR/two.ini" "$BATS_TEST_TMPDIR/four.ini"
  run changed_lines "$INI" "$BATS_TEST_TMPDIR/four.ini"
  [ "$output" = $'< port=3389\n> port=3390\n< max_bpp=32\n> max_bpp=24' ]
}

@test "ini_set returns 2 for values and names that do not fit into one line" {
  run ini_set "$INI" Globals port $'1\n[Evil]'
  [ "$status" -eq 2 ]
  [ "$output" = "" ]
  run ini_set "$INI" Globals port $'1\r'
  [ "$status" -eq 2 ]
  run ini_set "$INI" Globals "a=b" 1
  [ "$status" -eq 2 ]
  run ini_set "$INI" Globals "[x" 1
  [ "$status" -eq 2 ]
  run ini_set "$INI" Globals ";x" 1
  [ "$status" -eq 2 ]
  run ini_set "$INI" $'G\nH' port 1
  [ "$status" -eq 2 ]
  run ini_set "$BATS_TEST_TMPDIR/missing.ini" Globals port 1
  [ "$status" -eq 2 ]
}

@test "the helper works under set -euo pipefail" {
  run bash -c 'set -euo pipefail; . "$1"; ini_set "$2" Globals port 3390 | grep -c "^port=3390$"; ini_get "$2" Globals port' _ "$BATS_TEST_DIRNAME/../lib/ini.sh" "$INI"
  [ "$status" -eq 0 ]
  [ "$output" = $'1\n3389' ]
}
