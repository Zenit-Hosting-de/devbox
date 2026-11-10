#!/usr/bin/env bats
# Tests of the xrdp plugin without root and without a real xrdp: the manifest, the
# plugin test of the core, and every script run with a request file against fixture INI
# files in a temporary folder and fake systemctl, apt-get, dpkg-query and sudo. The flow
# through the CLI runs in test/testscript/xrdp.txtar.

load helpers

PLUGIN="$BATS_TEST_DIRNAME/.."

setup() {
  export HOME="$BATS_TEST_TMPDIR/home"
  ETC="$BATS_TEST_TMPDIR/etc"
  FAKE="$BATS_TEST_TMPDIR/fake"
  MSGS="$BATS_TEST_TMPDIR/messages.jsonl"
  BACKUP="$BATS_TEST_TMPDIR/backup"
  mkdir -p "$HOME" "$ETC/xrdp" "$ETC/polkit-1/rules.d"
  write_xrdp_ini "$ETC/xrdp/xrdp.ini"
  write_sesman_ini "$ETC/xrdp/sesman.ini"
  cp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  fake_commands "$FAKE"
  : >"$MSGS"
  RULE="$ETC/polkit-1/rules.d/45-devbox-xrdp.rules"
}

# installed: marks xrdp and the desktop as installed, with their units.
installed() {
  local p
  for p in xrdp xorgxrdp xfce4 dbus-x11; do : >"$FAKE/pkgs/$p"; done
  : >"$FAKE/units/xrdp"
  : >"$FAKE/units/xrdp-sesman"
}

# running: the units are enabled and active.
running() {
  local u
  for u in xrdp xrdp-sesman; do
    : >"$FAKE/enabled/$u"
    : >"$FAKE/active/$u"
  done
}

changed_lines() { diff "$1" "$2" | grep '^[<>]' || true; }

@test "manifest, scripts and translations are valid" {
  devbox plugin validate "$PLUGIN"
}

@test "plugin test: checks print JSON, dry runs change nothing, a second run changes nothing" {
  PATH="$FAKE/bin:$PATH" run devbox plugin test --apply \
    --set xrdp.manager=apt --set xrdp.port=3390 --set xrdp.sound=true \
    --set "xrdp.xrdpIni=$ETC/xrdp/xrdp.ini" --set "xrdp.sesmanIni=$ETC/xrdp/sesman.ini" \
    --set "xrdp.polkitRuleFile=$RULE" "$PLUGIN"
  echo "$output"
  [ "$status" -eq 0 ]
  [[ "$output" == *"check"*"xrdp.status"*"missing"* ]]
  [[ "$output" == *"dry-run"*"xrdp.install"*"5 planned changes"* ]]
  [[ "$output" == *"dry-run"*"xrdp.restart"*"2 planned changes"* ]]
  [[ "$output" == *"repeat"*"xrdp.install"*"no changes"* ]]
  [[ "$output" == *"repeat"*"xrdp.configure"*"no changes"* ]]
  [[ "$output" == *"repeat"*"xrdp.polkit"*"no changes"* ]]
  [[ "$output" == *"repeat"*"xrdp.sound"*"no changes"* ]]
  [[ "$output" == *"repeat"*"xrdp.enable"*"no changes"* ]]
  [[ "$output" == *"skipped"*"xrdp.restart"*"danger"* ]]
  [[ "$output" == *"skipped"*"xrdp.disable"*"danger"* ]]
  [[ "$output" != *"failed"* ]]
  # the applied run reached the fixture files, and only the keys of the plugin
  run changed_lines "$BATS_TEST_TMPDIR/xrdp.orig" "$ETC/xrdp/xrdp.ini"
  [ "$output" = $'< port=3389\n> port=3390' ]
  run changed_lines "$BATS_TEST_TMPDIR/sesman.orig" "$ETC/xrdp/sesman.ini"
  [ "$output" = $'< AllowRootLogin=true\n> AllowRootLogin=false' ]
}

@test "plugin test with the defaults of the manifest: dry runs only" {
  PATH="$FAKE/bin:$PATH" run devbox plugin test --set xrdp.manager=apt \
    --set "xrdp.xrdpIni=$ETC/xrdp/xrdp.ini" --set "xrdp.sesmanIni=$ETC/xrdp/sesman.ini" \
    --set "xrdp.polkitRuleFile=$RULE" "$PLUGIN"
  echo "$output"
  [ "$status" -eq 0 ]
  [[ "$output" != *"failed"* ]]
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cmp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  [ ! -e "$RULE" ]
  [ ! -e "$FAKE/calls.log" ]
}

# --- configure -------------------------------------------------------------------------

@test "configure: the dry run reports every key and changes no file" {
  S_PORT=3390 S_SECURITY=tls S_BPP=24 run_script actions/configure.sh 1
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cmp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  [ ! -e "$HOME/.xsession" ]
  [ ! -e "$BACKUP" ]
  grep -qF '"summary":"[Globals] port=3390 (was 3389)"' "$MSGS"
  grep -qF '"summary":"[Globals] security_layer=tls (was negotiate)"' "$MSGS"
  grep -qF '"summary":"[Globals] max_bpp=24 (was 32)"' "$MSGS"
  grep -qF '"summary":"[Security] AllowRootLogin=false (was true)"' "$MSGS"
  grep -qF '"summary":"devbox:xrdp block"' "$MSGS"
  [ "$(changes)" -eq 5 ]
  grep -qF '"summary":"The configuration of xrdp would be applied","type":"result"' "$MSGS"
}

@test "configure: sets only its keys, everything else stays byte for byte" {
  S_PORT=3390 S_SECURITY=tls S_BPP=24 run_script actions/configure.sh
  run changed_lines "$BATS_TEST_TMPDIR/xrdp.orig" "$ETC/xrdp/xrdp.ini"
  [ "$output" = $'< port=3389\n> port=3390\n< security_layer=negotiate\n> security_layer=tls\n< max_bpp=32\n> max_bpp=24' ]
  run changed_lines "$BATS_TEST_TMPDIR/sesman.orig" "$ETC/xrdp/sesman.ini"
  [ "$output" = $'< AllowRootLogin=true\n> AllowRootLogin=false' ]
  # the same key name in the session sections is untouched
  . "$PLUGIN/lib/ini.sh"
  [ "$(ini_get "$ETC/xrdp/xrdp.ini" Xorg port)" = -1 ]
  [ "$(ini_get "$ETC/xrdp/xrdp.ini" Xvnc port)" = -1 ]
  [ "$(ini_get "$ETC/xrdp/sesman.ini" Globals ListenPort)" = 3350 ]
  [ "$(wc -l <"$ETC/xrdp/xrdp.ini")" -eq "$(wc -l <"$BATS_TEST_TMPDIR/xrdp.orig")" ]
  grep -qF '"restartNeeded":true' "$MSGS"
  grep -qF '"level":"info","text":"A restart of xrdp applies the new values (xrdp.restart)."' "$MSGS"
}

@test "configure: the originals are in the backup folder before the first change" {
  S_PORT=3390 run_script actions/configure.sh
  cmp "$BACKUP$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cmp "$BACKUP$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  # a later run of the session keeps the first original
  S_PORT=3391 run_script actions/configure.sh
  cmp "$BACKUP$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
}

@test "configure: a second run reports no change and writes nothing" {
  S_PORT=3390 S_BPP=16 run_script actions/configure.sh
  first=$(cat "$ETC/xrdp/xrdp.ini" "$ETC/xrdp/sesman.ini" "$HOME/.xsession" | cksum)
  rm -rf "$BACKUP"
  reset_messages
  S_PORT=3390 S_BPP=16 run_script actions/configure.sh
  [ "$(changes)" -eq 0 ]
  [ "$(cat "$ETC/xrdp/xrdp.ini" "$ETC/xrdp/sesman.ini" "$HOME/.xsession" | cksum)" = "$first" ]
  [ ! -e "$BACKUP" ]
  grep -qF '"restartNeeded":false' "$MSGS"
  reset_messages
  S_PORT=3390 S_BPP=16 run_script actions/configure.sh 1
  [ "$(changes)" -eq 0 ]
}

@test "configure: files that match the settings are not written at all" {
  S_ROOT=true run_script actions/configure.sh
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cmp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  [ ! -e "$BACKUP$ETC" ]
  # only the session block is new
  [ "$(changes)" -eq 1 ]
}

@test "configure: going back to the old values restores the files byte for byte" {
  S_PORT=3390 S_SECURITY=rdp S_BPP=15 run_script actions/configure.sh
  refute cmp -s "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  S_ROOT=true run_script actions/configure.sh
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cmp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
}

@test "configure: a key that is missing is added to its section" {
  grep -v '^max_bpp=' "$BATS_TEST_TMPDIR/xrdp.orig" >"$ETC/xrdp/xrdp.ini"
  cp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.nobpp"
  S_BPP=24 run_script actions/configure.sh
  grep -qF '"summary":"[Globals] max_bpp=24 (was not set)"' "$MSGS"
  run changed_lines "$BATS_TEST_TMPDIR/xrdp.nobpp" "$ETC/xrdp/xrdp.ini"
  [ "$output" = '> max_bpp=24' ]
  . "$PLUGIN/lib/ini.sh"
  [ "$(ini_get "$ETC/xrdp/xrdp.ini" Globals max_bpp)" = 24 ]
}

@test "configure: the desktop starts from a marked block, in remote sessions only" {
  printf '# my own line\nexport FOO=bar\n' >"$HOME/.xsession"
  run_script actions/configure.sh
  run cat "$HOME/.xsession"
  [ "${lines[0]}" = '# my own line' ]
  [ "${lines[1]}" = 'export FOO=bar' ]
  [[ "$output" == *'# >>> devbox:xrdp >>>'*'if [ -n "${XRDP_SESSION:-}" ]; then'*'  exec startxfce4'*'fi'*'# <<< devbox:xrdp <<<' ]]
  bash -n "$HOME/.xsession"
  cmp "$BACKUP$HOME/.xsession" <(printf '# my own line\nexport FOO=bar\n')
}

@test "configure: desktop none removes the block and leaves the file as before" {
  printf '# my own line\nexport FOO=bar\n' >"$HOME/.xsession"
  run_script actions/configure.sh
  grep -qF 'devbox:xrdp' "$HOME/.xsession"
  S_DESKTOP=none run_script actions/configure.sh
  cmp "$HOME/.xsession" <(printf '# my own line\nexport FOO=bar\n')
}

@test "configure: desktop none deletes a session file that held only the block" {
  run_script actions/configure.sh
  [ -f "$HOME/.xsession" ]
  reset_messages
  S_DESKTOP=none run_script actions/configure.sh 1
  [ -f "$HOME/.xsession" ]
  grep -qF '"op":"delete"' "$MSGS"
  reset_messages
  S_DESKTOP=none run_script actions/configure.sh
  [ ! -e "$HOME/.xsession" ]
  grep -qF '"op":"delete"' "$MSGS"
  reset_messages
  S_DESKTOP=none run_script actions/configure.sh
  [ "$(changes)" -eq 0 ]
}

@test "configure: without the INI files the run stops with exit 3, the dry run plans" {
  rm "$ETC/xrdp/xrdp.ini"
  run run_script actions/configure.sh
  [ "$status" -eq 3 ]
  grep -qF 'does not exist. Install xrdp first' "$MSGS"
  [ ! -e "$HOME/.xsession" ]
  cmp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  reset_messages
  run run_script actions/configure.sh 1
  [ "$status" -eq 0 ]
  grep -qF '"summary":"set 3 keys once xrdp is installed"' "$MSGS"
  # a missing sesman.ini stops the run before xrdp.ini is written
  cp "$BATS_TEST_TMPDIR/xrdp.orig" "$ETC/xrdp/xrdp.ini"
  rm "$ETC/xrdp/sesman.ini"
  reset_messages
  S_PORT=3390 run run_script actions/configure.sh
  [ "$status" -eq 3 ]
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  [ "$(changes)" -eq 0 ]
}

@test "configure: invalid settings stop with exit 2 before any change" {
  S_PORT=70000 run run_script actions/configure.sh
  [ "$status" -eq 2 ]
  S_PORT=0 run run_script actions/configure.sh
  [ "$status" -eq 2 ]
  S_SECURITY=none run run_script actions/configure.sh
  [ "$status" -eq 2 ]
  S_BPP=12 run run_script actions/configure.sh
  [ "$status" -eq 2 ]
  S_SESSION_COMMAND='startxfce4; rm -rf x' run run_script actions/configure.sh
  [ "$status" -eq 2 ]
  S_SESSION_COMMAND='$(id)' run run_script actions/configure.sh
  [ "$status" -eq 2 ]
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
  cmp "$ETC/xrdp/sesman.ini" "$BATS_TEST_TMPDIR/sesman.orig"
  [ ! -e "$HOME/.xsession" ]
  [ "$(changes)" -eq 0 ]
}

@test "configure: mode of the INI files stays" {
  chmod 640 "$ETC/xrdp/sesman.ini"
  run_script actions/configure.sh
  [ "$(stat -c %a "$ETC/xrdp/sesman.ini")" = 640 ]
}

# --- polkit ----------------------------------------------------------------------------

@test "polkit: the dry run plans the rule and writes nothing" {
  run_script actions/polkit.sh 1
  [ ! -e "$RULE" ]
  grep -qF '"op":"create"' "$MSGS"
  [ "$(changes)" -eq 1 ]
}

@test "polkit: the rule allows exactly the listed actions for the user" {
  run_script actions/polkit.sh
  run cat "$RULE"
  [ "${lines[0]}" = '// >>> devbox:xrdp >>>' ]
  [ "${lines[-1]}" = '// <<< devbox:xrdp <<<' ]
  [[ "$output" == *'"org.freedesktop.color-manager.create-device",'* ]]
  [[ "$output" == *'"org.freedesktop.NetworkManager.wifi.scan"'* ]]
  [[ "$output" == *"subject.user == \"$(id -un)\" && allowed.indexOf(action.id) >= 0"* ]]
  [ "$(grep -c 'polkit.Result.YES' "$RULE")" -eq 1 ]
  [ "$(grep -c '^    "' "$RULE")" -eq 2 ]
  # nothing wider than the list: no wildcard, no prefix match, no admin rule
  refute grep -qE 'indexOf\("|\.match\(|AUTH_ADMIN|addAdminRule|\*' "$RULE"
  [ "$(stat -c %a "$RULE")" = 644 ]
}

@test "polkit: a group can take the place of the user" {
  S_POLKIT_GROUP=remote-users run_script actions/polkit.sh
  grep -qF 'if (subject.isInGroup("remote-users") && allowed.indexOf(action.id) >= 0) {' "$RULE"
  refute grep -qF 'subject.user' "$RULE"
}

@test "polkit: a second run reports no change, a new list updates with a backup" {
  run_script actions/polkit.sh
  cp "$RULE" "$BATS_TEST_TMPDIR/rule.first"
  reset_messages
  run_script actions/polkit.sh
  [ "$(changes)" -eq 0 ]
  cmp "$RULE" "$BATS_TEST_TMPDIR/rule.first"
  S_POLKIT_ACTIONS='["org.freedesktop.color-manager.create-device"]' run_script actions/polkit.sh
  grep -qF '"op":"update"' "$MSGS"
  refute grep -qF wifi.scan "$RULE"
  cmp "$BACKUP$RULE" "$BATS_TEST_TMPDIR/rule.first"
}

@test "polkit: switched off, the rule is removed again" {
  run_script actions/polkit.sh
  [ -f "$RULE" ]
  reset_messages
  S_POLKIT=false run_script actions/polkit.sh 1
  [ -f "$RULE" ]
  grep -qF '"op":"delete"' "$MSGS"
  S_POLKIT=false run_script actions/polkit.sh
  [ ! -e "$RULE" ]
  reset_messages
  S_POLKIT=false run_script actions/polkit.sh
  [ "$(changes)" -eq 0 ]
  # an empty list of actions is no rule either
  S_POLKIT_ACTIONS='[]' run_script actions/polkit.sh
  [ ! -e "$RULE" ]
}

@test "polkit: a file that DevBox did not write is neither overwritten nor deleted" {
  printf 'polkit.addRule(function(action, subject) {});\n' >"$RULE"
  cp "$RULE" "$BATS_TEST_TMPDIR/rule.foreign"
  run run_script actions/polkit.sh
  [ "$status" -eq 1 ]
  S_POLKIT=false run run_script actions/polkit.sh
  [ "$status" -eq 1 ]
  cmp "$RULE" "$BATS_TEST_TMPDIR/rule.foreign"
  [ "$(changes)" -eq 0 ]
}

@test "polkit: names that could break out of the rule are refused" {
  S_POLKIT_ACTIONS='["a\") || true || (\""]' run run_script actions/polkit.sh
  [ "$status" -eq 2 ]
  S_POLKIT_ACTIONS='["org.example.*"]' run run_script actions/polkit.sh
  [ "$status" -eq 2 ]
  S_POLKIT_GROUP='x") || true || ("' run run_script actions/polkit.sh
  [ "$status" -eq 2 ]
  S_RULE_FILE='relative/rule.rules' run run_script actions/polkit.sh
  [ "$status" -eq 2 ]
  [ ! -e "$RULE" ]
}

# --- packages --------------------------------------------------------------------------

@test "install: dry run plans the missing packages, the run installs them once" {
  : >"$FAKE/pkgs/dbus-x11"
  run_script actions/install.sh 1
  [ "$(changes)" -eq 3 ]
  [ ! -e "$FAKE/calls.log" ]
  reset_messages
  run_script actions/install.sh
  [ "$(cat "$FAKE/calls.log")" = $'apt-get update\napt-get install -y xrdp xorgxrdp xfce4' ]
  grep -qF '"summary":"3 packages installed"' "$MSGS"
  reset_messages
  run_script actions/install.sh
  [ "$(changes)" -eq 0 ]
  grep -qF '"summary":"xrdp is installed"' "$MSGS"
  [ "$(wc -l <"$FAKE/calls.log")" -eq 2 ]
}

@test "install: desktop none leaves the desktop packages out" {
  S_DESKTOP=none run_script actions/install.sh
  [ "$(cat "$FAKE/calls.log")" = $'apt-get update\napt-get install -y xrdp xorgxrdp' ]
}

@test "install: no package manager or no packages for it stop with exit 3" {
  S_MANAGER=pacman run run_script actions/install.sh
  [ "$status" -eq 3 ]
  S_PACKAGES='{ "apt": [] }' run run_script actions/install.sh
  [ "$status" -eq 3 ]
  grep -qF 'No xrdp packages are set for apt' "$MSGS"
  [ "$(changes)" -eq 0 ]
  [ ! -e "$FAKE/calls.log" ]
}

@test "sound: off does nothing, on installs the module once" {
  run_script actions/sound.sh
  [ "$(changes)" -eq 0 ]
  [ ! -e "$FAKE/calls.log" ]
  grep -qF '"summary":"Sound is switched off"' "$MSGS"
  S_SOUND=true run_script actions/sound.sh 1
  [ "$(changes)" -eq 1 ]
  [ ! -e "$FAKE/calls.log" ]
  reset_messages
  S_SOUND=true run_script actions/sound.sh
  [ "$(cat "$FAKE/calls.log")" = $'apt-get update\napt-get install -y pipewire-module-xrdp' ]
  grep -qF '"kind":"relogin"' "$MSGS"
  reset_messages
  S_SOUND=true run_script actions/sound.sh
  [ "$(changes)" -eq 0 ]
  [ "$(wc -l <"$FAKE/calls.log")" -eq 2 ]
}

@test "sound: without a package for the package manager the run stops with exit 3" {
  S_SOUND=true S_SOUND_PACKAGES='{ "apt": [] }' run run_script actions/sound.sh
  [ "$status" -eq 3 ]
  grep -qF 'No sound packages are set for apt' "$MSGS"
  [ ! -e "$FAKE/calls.log" ]
}

# --- services --------------------------------------------------------------------------

@test "enable: starts and enables the services once" {
  installed
  run_script actions/enable.sh 1
  [ "$(changes)" -eq 2 ]
  [ ! -e "$FAKE/calls.log" ]
  reset_messages
  run_script actions/enable.sh
  [ "$(cat "$FAKE/calls.log")" = 'systemctl enable --now xrdp-sesman xrdp' ]
  reset_messages
  run_script actions/enable.sh
  [ "$(changes)" -eq 0 ]
  [ "$(wc -l <"$FAKE/calls.log")" -eq 1 ]
}

@test "enable: only the service that is off is switched on" {
  installed
  : >"$FAKE/enabled/xrdp-sesman"
  : >"$FAKE/active/xrdp-sesman"
  run_script actions/enable.sh
  [ "$(cat "$FAKE/calls.log")" = 'systemctl enable --now xrdp' ]
}

@test "enable and restart: without the services the run stops with exit 3, the dry run plans" {
  run run_script actions/enable.sh
  [ "$status" -eq 3 ]
  run run_script actions/restart.sh
  [ "$status" -eq 3 ]
  [ "$(changes)" -eq 0 ]
  [ ! -e "$FAKE/calls.log" ]
  run_script actions/enable.sh 1
  run_script actions/restart.sh 1
  [ "$(changes)" -eq 4 ]
  [ ! -e "$FAKE/calls.log" ]
}

@test "restart: restarts the services in the order of the setting" {
  installed
  running
  run_script actions/restart.sh 1
  [ ! -e "$FAKE/calls.log" ]
  run_script actions/restart.sh
  [ "$(cat "$FAKE/calls.log")" = 'systemctl restart xrdp-sesman xrdp' ]
  grep -qF '"summary":"xrdp restarted"' "$MSGS"
}

@test "disable: stops and disables the services, a second run does nothing" {
  installed
  running
  run_script actions/disable.sh 1
  [ "$(changes)" -eq 2 ]
  [ ! -e "$FAKE/calls.log" ]
  [ -e "$FAKE/active/xrdp" ]
  reset_messages
  run_script actions/disable.sh
  [ "$(cat "$FAKE/calls.log")" = 'systemctl disable --now xrdp-sesman xrdp' ]
  [ ! -e "$FAKE/active/xrdp" ]
  [ ! -e "$FAKE/enabled/xrdp" ]
  reset_messages
  run_script actions/disable.sh
  [ "$(changes)" -eq 0 ]
  [ "$(wc -l <"$FAKE/calls.log")" -eq 1 ]
  # packages and configuration stay
  [ -e "$FAKE/pkgs/xrdp" ]
  cmp "$ETC/xrdp/xrdp.ini" "$BATS_TEST_TMPDIR/xrdp.orig"
}

@test "disable: nothing to do when xrdp is not installed" {
  run_script actions/disable.sh
  [ "$(changes)" -eq 0 ]
  grep -qF '"summary":"xrdp is switched off"' "$MSGS"
}

# --- status ----------------------------------------------------------------------------

@test "status: missing packages" {
  run run_script checks/status.sh
  [ "$status" -eq 0 ]
  [[ "$output" == '{"status":"missing","summary":"Not installed: xrdp, xorgxrdp, xfce4, dbus-x11",'* ]]
  [[ "$output" == *'"action":"xrdp.install"'* ]]
  [ "$(wc -l <<<"$output")" -eq 1 ]
}

@test "status: ok when installed, running and configured as set" {
  installed
  running
  run_script actions/configure.sh
  run run_script checks/status.sh
  [ "$status" -eq 0 ]
  [[ "$output" == '{"status":"ok","summary":"xrdp is running on port 3389",'* ]]
  [[ "$output" == *'"active":true,"enabled":true,"port":3389,'* ]]
  [[ "$output" == *'"config":{"port":"3389","securityLayer":"negotiate","maxBpp":"32","allowRootLogin":"false"},"differences":[]'* ]]
  [[ "$output" != *'"hints"'* ]]
}

@test "status: warns when the INI values differ from the settings" {
  installed
  running
  run_script actions/configure.sh
  S_PORT=3390 S_BPP=24 run run_script checks/status.sh
  [[ "$output" == '{"status":"warn","summary":"Configuration differs from the settings: port, max_bpp",'* ]]
  [[ "$output" == *'"differences":["port","max_bpp"]'* ]]
  [[ "$output" == *'"action":"xrdp.configure"'* ]]
  # the port of the session sections does not count
  S_PORT=-1 run run_script checks/status.sh
  [[ "$output" == *'"differences":["port"]'* ]]
}

@test "status: warns when the services are off or the session start is missing" {
  installed
  run_script actions/configure.sh
  run run_script checks/status.sh
  [[ "$output" == '{"status":"warn","summary":"xrdp is switched off",'* ]]
  [[ "$output" == *'"active":false,"enabled":false'* ]]
  [[ "$output" == *'"action":"xrdp.enable"'* ]]
  running
  rm "$HOME/.xsession"
  run run_script checks/status.sh
  [[ "$output" == *'"differences":["session start"]'* ]]
  S_DESKTOP=none run run_script checks/status.sh
  [[ "$output" != *'session start'* ]]
}

@test "status: an INI file that cannot be read is a warning, not a failure" {
  installed
  running
  rm "$ETC/xrdp/sesman.ini"
  run run_script checks/status.sh
  [ "$status" -eq 0 ]
  [[ "$output" == '{"status":"warn","summary":"Cannot read '*'/etc/xrdp/sesman.ini",'* ]]
}

@test "status: changes nothing" {
  installed
  before=$(find "$BATS_TEST_TMPDIR/etc" "$HOME" "$FAKE" -type f -exec cksum {} + | sort)
  run_script checks/status.sh >/dev/null
  [ "$(find "$BATS_TEST_TMPDIR/etc" "$HOME" "$FAKE" -type f -exec cksum {} + | sort)" = "$before" ]
}
