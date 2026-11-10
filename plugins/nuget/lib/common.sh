#!/usr/bin/env bash
# Shared functions of the nuget plugin. Sourced by checks and consumers.
# Uses only devbox sdk and the commands listed in requires.commands.
#
# Managed parts of NuGet.Config (marked blocks with XML comments as markers):
#   <packageSources>            nuget:NAME              <add key=KEY value=URL />
#   <packageSourceCredentials>  nuget:NAME:credentials  <KEY> with Username and
#                                                       ClearTextPassword (mode file)
# A section the file lacks is added as the block nuget-section:SECTION (packageSources
# with the default source) and removed again when nothing else is left in it. A file
# that does not exist is created as the block nuget-created around <configuration> and
# deleted again when it holds nothing else. In mode env the shell environment file
# exports NuGetPackageSourceCredentials_KEY in the block nuget:NAME.

# shellcheck disable=SC2034 # settings are used by the scripts that source this file
sdk() { "$DEVBOX_BIN" sdk "$@"; }

CONFIG=$(sdk setting configPath)
MODE=$(sdk setting mode)
ENV_FILE=$(sdk setting envFile)
SOURCE_NAME=$(sdk setting sourceName)
DEFAULT_SOURCE=$(sdk setting defaultSource)
DEFAULT_SOURCE_NAME=$(sdk setting defaultSourceName)

is_dry_run() { [ "${DEVBOX_DRY_RUN:-0}" = 1 ]; }

# shellcheck source=names.sh
. "$DEVBOX_PLUGIN_DIR/lib/names.sh"

fail() {
  sdk emit log error "$1"
  exit 2
}

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\t'/\\t}
  printf '"%s"' "$s"
}

# squeeze TEXT: TEXT without any white space, for comparisons that ignore layout.
squeeze() { printf '%s' "${1//[[:space:]]/}"; }

# xml_markers NAME: sets BEGIN and END to the XML marker lines of the block NAME.
xml_markers() {
  BEGIN="<!-- >>> devbox:$1 >>> -->"
  END="<!-- <<< devbox:$1 <<< -->"
}

# has_xml_block NAME: whether the block NAME is in the configuration file.
has_xml_block() {
  local content
  [ -f "$CONFIG" ] || return 1
  xml_markers "$1"
  content=$(<"$CONFIG")
  [[ $content == *"$BEGIN"*"$END"* ]]
}

# has_line TEXT: a line equal to TEXT (ignoring white space) outside of XML comments.
has_line() {
  [ -f "$CONFIG" ] || return 1
  sdk has-line "$CONFIG" "$1" --comment xml 2>/dev/null
}

# default_line: the <add> element of the default source, empty without one.
default_line() {
  [ -n "$DEFAULT_SOURCE" ] || return 0
  printf '<add key="%s" value="%s" protocolVersion="3" />' \
    "$(xml_escape "$DEFAULT_SOURCE_NAME")" "$(xml_escape "$DEFAULT_SOURCE")"
}

# ensure_file: creates NuGet.Config as the block nuget-created when there is none.
# A dry run only reports it.
ensure_file() {
  if [ -e "$CONFIG" ]; then
    has_line "</configuration>" || fail "$CONFIG has no line </configuration>; cannot add to it"
    return 0
  fi
  printf '<configuration>\n</configuration>\n' | sdk block "$CONFIG" nuget-created --comment xml --mode 600
}

# ensure_section NAME: makes sure <NAME> … </NAME> exists in the file.
ensure_section() {
  local content body
  has_line "</$1>" && return 0
  content=$(<"$CONFIG")
  if [[ $content =~ \<$1[[:space:]]*/\> ]]; then
    fail "$CONFIG has an empty <$1/> element; write it as <$1></$1> first"
  fi
  body="  <$1>"$'\n'
  if [ "$1" = packageSources ] && [ -n "$DEFAULT_SOURCE" ]; then
    body+="    $(default_line)"$'\n'
  fi
  body+="  </$1>"$'\n'
  printf '%s' "$body" | sdk block "$CONFIG" "nuget-section:$1" --comment xml --before "</configuration>" --mode 600
}

# put_block SECTION NAME: writes the block NAME (body on stdin) into <SECTION>.
# A dry run on a file that lacks the section only reports the change.
put_block() {
  local section=$1 name=$2 body
  body=$(cat)
  if is_dry_run && ! has_line "</$section>"; then
    sdk emit change file update "$CONFIG" "devbox:$name block"
    return 0
  fi
  is_dry_run || ensure_section "$section"
  printf '%s\n' "$body" | sdk block "$CONFIG" "$name" --comment xml --before "</$section>" --mode 600
}

# drop_xml_block NAME: removes the block NAME from the configuration file.
drop_xml_block() {
  has_xml_block "$1" || return 0
  sdk block-remove "$CONFIG" "$1" --comment xml
}

# block_body NAME: the text between the markers of the block NAME.
block_body() {
  local content rest
  xml_markers "$1"
  content=$(<"$CONFIG")
  rest=${content#*"$BEGIN"}
  printf '%s' "${rest%%"$END"*}"
}

# drop_empty_sections: removes the sections DevBox added once nothing but their
# original content is left in them, then the file DevBox created once it holds
# nothing else.
drop_empty_sections() {
  local s want content
  [ -f "$CONFIG" ] || return 0
  for s in packageSources packageSourceCredentials; do
    has_xml_block "nuget-section:$s" || continue
    want="<$s>"
    [ "$s" != packageSources ] || want+=$(default_line)
    want+="</$s>"
    if [ "$(squeeze "$(block_body "nuget-section:$s")")" = "$(squeeze "$want")" ]; then
      sdk block-remove "$CONFIG" "nuget-section:$s" --comment xml
    fi
  done
  is_dry_run && return 0
  has_xml_block nuget-created || return 0
  xml_markers nuget-created
  content=$(<"$CONFIG")
  if [ "$(squeeze "$content")" = "$(squeeze "$BEGIN<configuration></configuration>$END")" ]; then
    rm -f "$CONFIG"
    sdk emit change file delete "$CONFIG" "created by devbox, empty after removing its blocks"
  fi
}

# secure_file: the file holds a password, so only the owner may read it.
secure_file() {
  local mode
  is_dry_run && return 0
  [ -e "$CONFIG" ] || return 0
  mode=$(stat -c %a "$CONFIG")
  if [ "$mode" != 600 ]; then
    chmod 600 "$CONFIG"
    sdk emit change file update "$CONFIG" "mode 0600"
  fi
}

# Shell environment file (mode env), with "#" markers as in the other consumers.

ENV_CREATED="nuget-created"

# has_env_block NAME: whether the block NAME is in the environment file.
has_env_block() {
  local content
  [ -f "$ENV_FILE" ] || return 1
  content=$(<"$ENV_FILE")
  [[ $content == *"# >>> devbox:$1 >>>"* ]]
}

# put_env_block NAME: writes the block NAME (body on stdin) into the environment file,
# which DevBox marks as its own when it creates it.
put_env_block() {
  is_dry_run || { [ ! -e "$ENV_FILE" ] || chmod 600 "$ENV_FILE"; }
  if [ ! -e "$ENV_FILE" ]; then
    printf 'File created by DevBox.\n' | sdk block "$ENV_FILE" "$ENV_CREATED" --comment hash --mode 600
  fi
  sdk block "$ENV_FILE" "$1" --comment hash --mode 600
}

# drop_env_block NAME: removes the block NAME; a file DevBox created that holds nothing
# else afterwards is deleted, a file that existed before stays.
drop_env_block() {
  local content rest
  has_env_block "$1" || return 0
  sdk block-remove "$ENV_FILE" "$1" --comment hash
  is_dry_run && return 0
  has_env_block "$ENV_CREATED" || return 0
  content=$(<"$ENV_FILE")
  rest=${content/"# >>> devbox:$ENV_CREATED >>>"*"# <<< devbox:$ENV_CREATED <<<"/}
  if [[ -z ${rest//[[:space:]]/} ]]; then
    rm -f "$ENV_FILE"
    sdk emit change file delete "$ENV_FILE" "created by devbox, empty after removing devbox:$1"
  fi
}
