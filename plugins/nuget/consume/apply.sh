#!/usr/bin/env bash
# Consumer: adds the feed of a registry credential as a package source to NuGet.Config.
#   mode file: the credential goes into <packageSourceCredentials> (ClearTextPassword,
#              the file gets mode 0600)
#   mode env:  the shell environment file (mode 0600) exports
#              NuGetPackageSourceCredentials_<key>=Username=…;Password=…
# The default source (nuget.org) stays; a file or section DevBox adds carries it.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

name=$(sdk instance)
base=$(sdk field url)
layout=$(sdk field layout 2>/dev/null) || layout=nexus
user=$(sdk field username 2>/dev/null) || user=""
repo=$(sdk option repository 2>/dev/null) || repo=""
url=$(sdk option url 2>/dev/null) || url=""
opt_key=$(sdk option sourceName 2>/dev/null) || opt_key=""

key=$(source_key "$opt_key" "$SOURCE_NAME" "$name")
[[ $key =~ ^[A-Za-z_][A-Za-z0-9_.-]*$ ]] || fail "package source name $key is not a valid XML element name"
if [ "$MODE" = env ] && [[ ! $key =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  fail "package source name $key cannot be part of an environment variable name; set the option sourceName"
fi
[ -n "$user" ] || fail "the credential $name has no field username, which NuGet needs for the source $key"
if ! feed=$(feed_url "$layout" "$base" "$repo" "$url" 2>/dev/null); then
  case "$layout" in
  nexus | artifactory) fail "layout $layout needs the option repository (or url)" ;;
  *) fail "layout $layout needs the option url with the full feed address" ;;
  esac
fi
secret=$(sdk secret)
if [ "$MODE" = env ] && [[ $secret == *';'* || $user == *';'* ]]; then
  fail "NuGetPackageSourceCredentials_$key cannot carry a ';' in user name or secret; use mode file"
fi

# Another source with the same key (the user's own or another instance's) is not
# overwritten.
if [ -f "$CONFIG" ]; then
  xml_markers "nuget:$name"
  content=$(<"$CONFIG")
  rest=${content/"$BEGIN"*"$END"/}
  if [[ $rest == *"<add key=\"$(xml_escape "$key")\""* ]]; then
    fail "$CONFIG has a package source $key already; set the option sourceName"
  fi
fi

e_key=$(xml_escape "$key")
sdk emit step "apply-$name" running "$key"
ensure_file
printf '    <add key="%s" value="%s" />\n' "$e_key" "$(xml_escape "$feed")" | put_block packageSources "nuget:$name"

if [ "$MODE" = env ]; then
  drop_xml_block "nuget:$name:credentials"
  drop_empty_sections
  # Built in shell variables and piped with the printf builtin: the secret never
  # appears in the arguments of a process.
  printf 'export NuGetPackageSourceCredentials_%s=%s\n' "$key" "$(shell_quote "Username=$user;Password=$secret")" |
    put_env_block "nuget:$name"
else
  printf '    <%s>\n      <add key="Username" value="%s" />\n      <add key="ClearTextPassword" value="%s" />\n    </%s>\n' \
    "$e_key" "$(xml_escape "$user")" "$(xml_escape "$secret")" "$e_key" |
    put_block packageSourceCredentials "nuget:$name:credentials"
  secure_file
  drop_env_block "nuget:$name"
fi
sdk emit step "apply-$name" "done" "$key"
sdk emit result "NuGet source $key written to $CONFIG ($MODE)"
