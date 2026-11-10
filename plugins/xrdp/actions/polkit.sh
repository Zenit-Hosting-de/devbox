#!/usr/bin/env bash
# Action: write the polkit rule for remote sessions, or remove it when the setting
# polkit is off. The rule allows exactly the listed actions, for one user or one group,
# and nothing else. The file belongs to this plugin as a whole: it starts and ends with
# the marker lines, and a file without them is never overwritten or deleted.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

begin='// >>> devbox:xrdp >>>'
end='// <<< devbox:xrdp <<<'

enabled=$(sdk setting polkit)
file=$(sdk setting polkitRuleFile)
group=$(sdk setting polkitGroup)
mapfile -t actions < <(setting_items polkitActions)

invalid() {
  sdk emit log error "$1"
  exit 2
}
[ -n "$file" ] || invalid "xrdp.polkitRuleFile must not be empty"
case "$file" in /*) ;; *) invalid "xrdp.polkitRuleFile must be an absolute path" ;; esac
# names go into a rule file that root owns: only plain identifiers are accepted
for a in "${actions[@]}"; do
  [[ $a =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || invalid "xrdp.polkitActions: not a polkit action: $a"
done
[[ $group =~ ^([a-z_][a-z0-9_-]*)?$ ]] || invalid "xrdp.polkitGroup is not a group name"
if [ -n "$group" ]; then
  subject="subject.isInGroup(\"$group\")"
  who="group $group"
else
  user=$(id -un)
  [[ $user =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]] || invalid "The user name cannot be written into a polkit rule"
  subject="subject.user == \"$user\""
  who="user $user"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
orig="$work/orig"
new="$work/new"

# rule: the content of the rule file.
rule() {
  local a sep=""
  printf '%s\n' "$begin"
  printf '// Written by the DevBox plugin xrdp (action xrdp.polkit); changes are overwritten.\n'
  printf '// Answers the listed actions for %s without a password prompt. Nothing else.\n' "$who"
  printf 'polkit.addRule(function(action, subject) {\n'
  printf '  var allowed = [\n'
  for a in "${actions[@]}"; do
    printf '%s    "%s"' "$sep" "$a"
    sep=$',\n'
  done
  printf '\n  ];\n'
  printf '  if (%s && allowed.indexOf(action.id) >= 0) {\n' "$subject"
  printf '    return polkit.Result.YES;\n'
  printf '  }\n'
  printf '});\n'
  printf '%s\n' "$end"
}

# is_ours COPY: the file starts with the marker line of this plugin.
is_ours() {
  local first=""
  IFS= read -r first <"$1" || true
  [ "$first" = "$begin" ]
}

want=present
if [ "$enabled" != true ] || [ "${#actions[@]}" -eq 0 ]; then
  want=absent
fi

state=$(file_state "$file")
if [ "$state" = hidden ]; then
  if is_dry_run; then
    if [ "$want" = present ]; then
      sdk emit change file update "$file" "polkit rule for ${#actions[@]} actions, written where it differs (only root can read the folder)"
      sdk emit result "The polkit rule would be written"
    else
      sdk emit change file delete "$file" "polkit rule removed if it exists (only root can read the folder)"
      sdk emit result "The polkit rule would be removed"
    fi
    exit 0
  fi
  if load_hidden "$file" "$orig"; then state=present; else state=absent; fi
elif [ "$state" = present ]; then
  cp "$file" "$orig"
fi

if [ "$state" = present ] && ! is_ours "$orig"; then
  sdk emit log error "$file was not written by DevBox and stays as it is. Another path can be set in xrdp.polkitRuleFile."
  exit 1
fi

if [ "$want" = absent ]; then
  if [ "$state" = absent ]; then
    sdk emit result "No polkit rule is set"
    exit 0
  fi
  sdk emit change file delete "$file" "polkit rule removed"
  if is_dry_run; then
    sdk emit result "The polkit rule would be removed"
    exit 0
  fi
  backup_from "$orig" "$file"
  for_path "${file%/*}" rm -f "$file"
  sdk emit result "The polkit rule is removed"
  exit 0
fi

rule >"$new"
if [ "$state" = present ] && [ "$(cat "$orig" && printf x)" = "$(cat "$new" && printf x)" ]; then
  sdk emit result "The polkit rule is in place" "{\"actions\":${#actions[@]}}"
  exit 0
fi
op=create
[ "$state" = present ] && op=update
sdk emit change file "$op" "$file" "polkit rule for ${#actions[@]} actions and $who"
if is_dry_run; then
  sdk emit result "The polkit rule would be written"
  exit 0
fi
[ "$state" = present ] && backup_from "$orig" "$file"
replace_file "$new" "$file" 644
sdk emit result "The polkit rule is written" "{\"actions\":${#actions[@]}}"
