#!/usr/bin/env bash
# Shared functions of the git plugin. Sourced by checks, actions and consumers.
# Uses only devbox sdk and the commands listed in requires.commands.
#
# Everything DevBox writes into the Git configuration file sits in marked blocks:
#   git:identity           [user] name and email
#   git:defaults           the values of the setting defaults
#   git:credential:NAME    [credential "https://host"] with the helper of one git-host
#   git-created            only when DevBox created the file; it is deleted again when
#                          nothing else is left in it

# shellcheck disable=SC2034 # settings are used by the scripts that source this file
sdk() { "$DEVBOX_BIN" sdk "$@"; }

# shellcheck source=gitconfig.sh
. "$DEVBOX_PLUGIN_DIR/lib/gitconfig.sh"

GITCONFIG=$(sdk setting gitconfig)
CRED_FILE=$(sdk setting credentialsFile)
HELPER=$(sdk setting credentialHelper)
CREATED_BLOCK="git-created"

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# fail TEXT: invalid parameters or settings (exit code 2).
fail() {
  sdk emit log error "$1"
  echo "$1" >&2
  exit 2
}

# git_get KEY: the effective value of KEY in the configuration file of the setting
# gitconfig (Git's global files when it is ~/.gitconfig).
git_get() {
  if [ "$GITCONFIG" = "$HOME/.gitconfig" ]; then
    git config --global --get "$1" 2>/dev/null
  else
    git config --file "$GITCONFIG" --get "$1" 2>/dev/null
  fi
}

# has_block FILE NAME: whether the marked block of NAME is in FILE.
has_block() {
  local content
  [ -f "$1" ] || return 1
  content=$(<"$1")
  [[ $content == *"# >>> devbox:$2 >>>"* ]]
}

# block_body FILE NAME: the lines inside the marked block of NAME.
block_body() {
  local line inside=false
  [ -f "$1" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
    "# >>> devbox:$2 >>>") inside=true ;;
    "# <<< devbox:$2 <<<") inside=false ;;
    *) ! $inside || printf '%s\n' "$line" ;;
    esac
  done <"$1"
}

# block_value FILE NAME VARIABLE: the last value of VARIABLE in the block (as written by
# gc_entry: a tab, the name, " = " and a quoted value). Empty values are skipped.
block_value() {
  local line value="" prefix=$'\t'"$3 = "
  while IFS= read -r line; do
    case "$line" in
    "$prefix"?*) value=$(gc_unquote "${line#"$prefix"}") ;;
    esac
  done < <(block_body "$1" "$2")
  printf '%s' "$value"
}

# block_origin FILE NAME: the address of the [credential "…"] section in the block.
block_origin() {
  local line sub
  while IFS= read -r line; do
    case "$line" in
    '[credential "'*'"]')
      sub=${line#\[credential }
      sub=${sub%\]}
      gc_unquote "$sub"
      return 0
      ;;
    esac
  done < <(block_body "$1" "$2")
}

# mark_created FILE: when FILE does not exist yet, records in a marked block of its own
# that DevBox creates it. Call it before the first block is written.
mark_created() {
  [ ! -e "$1" ] || return 0
  printf '# File created by DevBox.\n' | sdk block "$1" "$CREATED_BLOCK"
}

# only_created_mark FILE: whether FILE holds nothing but the creation mark (and blanks).
only_created_mark() {
  local content rest
  has_block "$1" "$CREATED_BLOCK" || return 1
  content=$(<"$1")
  rest=${content/"# >>> devbox:$CREATED_BLOCK >>>"*"# <<< devbox:$CREATED_BLOCK <<<"/}
  [[ -z ${rest//[[:space:]]/} ]]
}

# drop_block FILE NAME: removes the marked block. A file that DevBox created (it carries
# the creation mark) and that holds nothing else afterwards is deleted; a file that
# existed before stays, so the system is as before.
drop_block() {
  local file=$1 name=$2
  has_block "$file" "$name" || return 0
  sdk block-remove "$file" "$name"
  if ! is_dry_run && only_created_mark "$file"; then
    rm -f "$file"
    sdk emit change file delete "$file" "created by devbox, empty after removing devbox:$name"
  fi
}

# helper_spec: the credential helper as Git's configuration names it. "store" keeps
# the tokens in the file of the setting credentialsFile.
helper_spec() {
  case "$HELPER" in
  '') fail "the setting credentialHelper is empty" ;;
  store) printf 'store --file %s' "$(shell_quote "$CRED_FILE")" ;;
  *) printf '%s' "$HELPER" ;;
  esac
}

# store_file SPEC: the file of a "store --file '…'" helper written by helper_spec.
store_file() {
  local f
  case "$1" in
  "store --file '"*"'")
    f=${1#store --file \'}
    f=${f%\'}
    printf '%s' "${f//\'\\\'\'/\'}"
    ;;
  *) return 1 ;;
  esac
}

# git_cred SPEC OPERATION: runs "git credential OPERATION" with SPEC as the only helper
# (stdin: Git's credential format). Helpers of the user are reset for the call, so the
# token goes to SPEC only; prompts are switched off.
git_cred() {
  GIT_TERMINAL_PROMPT=0 GIT_ASKPASS='' git -c credential.helper= -c "credential.helper=$1" credential "$2"
}

# stored_secret SPEC HOST USER: the password the helper has for the host, empty when
# none. The value stays in the shell; it is never printed to a terminal or log.
stored_secret() {
  local out line
  out=$(printf 'protocol=https\nhost=%s\nusername=%s\n\n' "$2" "$3" | git_cred "$1" fill 2>/dev/null) || return 0
  while IFS= read -r line; do
    case "$line" in
    password=*) printf '%s' "${line#password=}" ;;
    esac
  done <<<"$out"
}

# backup_file FILE: keeps the first original of FILE in this session's backup folder
# (same layout as devbox sdk block), before Git's own credential helper changes it.
backup_file() {
  local target
  [ -n "${DEVBOX_BACKUP_DIR:-}" ] && [ -f "$1" ] || return 0
  target="$DEVBOX_BACKUP_DIR$1"
  [ ! -e "$target" ] || return 0
  mkdir -p "${target%/*}"
  (
    umask 077
    cp -p "$1" "$target"
  )
}

# forget SPEC HOST USER: removes the token of USER at HOST from the helper SPEC, if it
# has one. A store file that is empty afterwards is deleted (it belongs to DevBox).
forget() {
  local spec=$1 host=$2 user=$3 file
  file=$(store_file "$spec") || file=""
  if [ -n "$(stored_secret "$spec" "$host" "$user")" ]; then
    if ! is_dry_run; then
      [ -z "$file" ] || backup_file "$file"
      printf 'protocol=https\nhost=%s\nusername=%s\n\n' "$host" "$user" | git_cred "$spec" reject
    fi
    if [ -n "$file" ]; then
      sdk emit change file update "$file" "token for https://$host removed"
    else
      sdk emit change other update "git credential $spec" "token for https://$host removed"
    fi
  fi
  if [ -n "$file" ] && ! is_dry_run && [ -f "$file" ] && [ ! -s "$file" ]; then
    rm -f "$file"
    sdk emit change file delete "$file" "no tokens left"
  fi
}

# secure_file FILE: a file with tokens may only be read by its owner.
secure_file() {
  local mode
  [ -f "$1" ] || return 0
  mode=$(stat -c %a "$1")
  if [ "$mode" != 600 ]; then
    chmod 600 "$1"
    sdk emit change file update "$1" "mode 0600"
  fi
}
