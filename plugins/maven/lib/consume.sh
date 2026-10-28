#!/usr/bin/env bash
# Shared functions of the registry consumer of the maven plugin (settings.xml).
# Uses only devbox sdk and the commands listed in requires.commands.
#
# Layout of the managed parts (each one a marked block, XML comments as markers):
#   <servers>        maven:NAME           <server> with id, username, password
#   <mirrors>        maven:NAME:mirror    <mirror>, only when the option mirrorOf is set
#   <profiles>       maven:NAME:profile   <profile> with the repository
#   <activeProfiles> maven:NAME:active    the profile switched on
# A section that the file lacks is created as the block maven-section:NAME and
# removed again when it is empty after a remove.

sdk() { "$DEVBOX_BIN" sdk "$@"; }

SETTINGS=$(sdk setting settingsPath)
SECTIONS=(servers mirrors profiles activeProfiles)

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

fail() {
  sdk emit log error "$1"
  exit 2
}

# xml_escape TEXT: TEXT for use as XML character data.
xml_escape() {
  local s=$1 amp='&amp;' lt='&lt;' gt='&gt;' quot='&quot;' apos='&apos;'
  s=${s//&/"$amp"}
  s=${s//</"$lt"}
  s=${s//>/"$gt"}
  s=${s//\"/"$quot"}
  s=${s//\'/"$apos"}
  printf '%s' "$s"
}

# check_name NAME: the instance name goes into XML comments and ids.
check_name() {
  [[ $1 =~ ^[A-Za-z0-9._-]+$ ]] || fail "instance name $1 has characters that Maven ids do not allow"
  [[ $1 != *--* ]] || fail "instance name $1 contains two hyphens, which XML comments do not allow"
}

# has_line FILE TEXT: is there a line that equals TEXT (ignoring white space)?
has_line() {
  local line
  [ -f "$1" ] || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    line=${line#"${line%%[![:space:]]*}"}
    line=${line%"${line##*[![:space:]]}"}
    [ "$line" = "$2" ] && return 0
  done <"$1"
  return 1
}

# repo_url: the address of the repository, from the option url or the layout.
repo_url() {
  local url layout repo base
  if url=$(sdk option url 2>/dev/null) && [ -n "$url" ]; then
    printf '%s' "$url"
    return 0
  fi
  base=$(sdk field url)
  base=${base%/}
  layout=$(sdk field layout 2>/dev/null) || layout=nexus
  case "$layout" in
  nexus | artifactory) ;;
  *) fail "layout $layout needs the option url with the full repository address" ;;
  esac
  repo=$(sdk option repository 2>/dev/null) || repo=""
  [ -n "$repo" ] || fail "set the option repository (or url) for the consumer maven"
  case "$layout" in
  nexus) printf '%s/repository/%s/' "$base" "$repo" ;;
  artifactory) printf '%s/artifactory/%s/' "$base" "$repo" ;;
  esac
}

# ensure_file: creates a minimal valid settings.xml (mode 0600) when there is none.
ensure_file() {
  if [ -e "$SETTINGS" ]; then
    has_line "$SETTINGS" "</settings>" || fail "$SETTINGS has no line </settings>; cannot add to it"
    return 0
  fi
  if ! is_dry_run; then
    mkdir -p "$(dirname "$SETTINGS")"
    (
      umask 077
      cat >"$SETTINGS" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<settings xmlns="http://maven.apache.org/SETTINGS/1.2.0"
          xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
          xsi:schemaLocation="http://maven.apache.org/SETTINGS/1.2.0 https://maven.apache.org/xsd/settings-1.2.0.xsd">
</settings>
XML
    )
  fi
  sdk emit change file create "$SETTINGS" "minimal settings.xml"
}

# ensure_section NAME: makes sure <NAME> … </NAME> exists in the file.
ensure_section() {
  local content
  has_line "$SETTINGS" "</$1>" && return 0
  content=$(<"$SETTINGS")
  if [[ $content =~ \<$1[[:space:]]*/\> ]]; then
    fail "$SETTINGS has an empty <$1/> element; write it as <$1></$1> first"
  fi
  printf '  <%s>\n  </%s>\n' "$1" "$1" | sdk block "$SETTINGS" "maven-section:$1" --before "</settings>" --mode 600
}

# put_block SECTION NAME: writes the block NAME (body on stdin) into <SECTION>.
# A dry run on a file that lacks the section only reports the change.
put_block() {
  local section=$1 name=$2 body
  body=$(cat)
  if is_dry_run && ! has_line "$SETTINGS" "</$section>"; then
    sdk emit change file update "$SETTINGS" "devbox:$name block"
    return 0
  fi
  is_dry_run || ensure_section "$section"
  printf '%s\n' "$body" | sdk block "$SETTINGS" "$name" --before "</$section>" --mode 600
}

# secure_file: the file holds a password, so only the owner may read it.
secure_file() {
  local mode
  is_dry_run && return 0
  mode=$(stat -c %a "$SETTINGS")
  if [ "$mode" != 600 ]; then
    chmod 600 "$SETTINGS"
    sdk emit change file update "$SETTINGS" "mode 0600"
  fi
}

# drop_empty_sections: removes the sections that devbox created and nobody filled.
drop_empty_sections() {
  local s content re
  [ -f "$SETTINGS" ] || return 0
  for s in "${SECTIONS[@]}"; do
    content=$(<"$SETTINGS")
    re="<!-- >>> devbox:maven-section:$s >>> -->[[:space:]]*<$s>[[:space:]]*</$s>[[:space:]]*<!-- <<< devbox:maven-section:$s <<< -->"
    if [[ $content =~ $re ]]; then
      sdk block-remove "$SETTINGS" "maven-section:$s"
    fi
  done
}
