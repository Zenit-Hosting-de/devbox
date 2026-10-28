#!/usr/bin/env bash
# shellcheck disable=SC2016 # the shell snippets written to startup files contain literal $HOME
# Action: load the environment files (envDir/*.sh) in every configured shell, and offer
# their simple export lines to systemd user sessions. Marked blocks only; the rest of
# the startup files stays as it is.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

env_dir=$(sdk setting envDir)
use_systemd=$(sdk setting systemd)
systemd_file=$(sdk setting systemdFile)
home=${HOME:?}

# dq PATH: PATH for use inside double quotes in a shell file; a path below $HOME
# keeps working when the home directory moves.
dq() {
  local p=$1 prefix=""
  case "$p" in
  "$home"/*)
    p=${p#"$home"/}
    prefix='$HOME/'
    ;;
  esac
  p=${p//\\/\\\\}
  p=${p//\"/\\\"}
  p=${p//\$/\\\$}
  p=${p//\`/\\\`}
  printf '%s%s' "$prefix" "$p"
}

# loader_body SHELL: the lines that source every file of the environment folder.
loader_body() {
  local dir glob
  dir=$(dq "$env_dir")
  glob='*.sh'
  [ "$1" = zsh ] && glob='*.sh(N)' # zsh fails on a pattern without match otherwise
  printf 'for f in "%s"/%s; do\n' "$dir" "$glob"
  printf '  if [ -r "$f" ]; then . "$f"; fi\n'
  printf 'done\n'
  printf 'unset f\n'
}

# systemd_lines: NAME=VALUE for every simple "export NAME=VALUE" line of the
# environment files. environment.d knows quoting and $VAR expansion, but no commands.
systemd_lines() {
  local f line
  for f in "$env_dir"/*.sh; do
    [ -r "$f" ] || continue
    while IFS= read -r line || [ -n "$line" ]; do
      if [[ $line =~ ^export[[:space:]]+([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
        case "${BASH_REMATCH[2]}" in
        *'$('* | *'`'*) continue ;;
        esac
        printf '%s=%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
      fi
    done <"$f"
  done
}

# file_sum FILE: checksum of a file, empty when it does not exist.
file_sum() { { cksum <"$1"; } 2>/dev/null || true; }

mapfile -t shells < <(setting_list shells)

if [ ! -d "$env_dir" ]; then
  is_dry_run || { mkdir -p "$env_dir" && chmod 700 "$env_dir"; }
  sdk emit change file create "$env_dir" "folder for environment files"
fi

for shell in "${shells[@]}"; do
  case "$shell" in
  bash | zsh) rc="$home/.${shell}rc" ;;
  *)
    sdk emit log error "Unsupported shell: $shell"
    exit 2
    ;;
  esac
  before=$(file_sum "$rc")
  sdk emit step "shell-$shell" running "Load environment files in $shell"
  loader_body "$shell" | sdk block "$rc" system
  sdk emit step "shell-$shell" "done" "Load environment files in $shell"
  if ! is_dry_run && [ "$(file_sum "$rc")" != "$before" ]; then
    sdk emit notice restart-shell "New terminals load the environment files right away, open ones after a restart."
  fi
done

if [ "$use_systemd" = true ]; then
  lines=$(systemd_lines)
  if [ -z "$lines" ] && [ ! -e "$systemd_file" ]; then
    sdk emit step systemd skipped "No variables for systemd yet"
  else
    before=$(file_sum "$systemd_file")
    sdk emit step systemd running "Provide variables to systemd sessions"
    {
      echo "# Simple export lines of $(dq "$env_dir")/*.sh. Run system.envLoader again after"
      echo "# a plugin changes its environment file."
      printf '%s' "$lines"
      [ -z "$lines" ] || echo
    } | sdk block "$systemd_file" system --mode 600
    sdk emit step systemd "done" "Provide variables to systemd sessions"
    if ! is_dry_run && [ "$(file_sum "$systemd_file")" != "$before" ]; then
      sdk emit notice relogin "systemd user sessions read the variables at the next login."
    fi
  fi
fi

if is_dry_run; then
  sdk emit result "The environment loader would be set up"
else
  sdk emit result "Environment loader is set up"
fi
