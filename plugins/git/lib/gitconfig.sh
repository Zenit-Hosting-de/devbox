#!/usr/bin/env bash
# Pure functions of the git plugin: quoting for Git's configuration format, config keys,
# credential addresses and JSON strings. Sourced by common.sh; needs nothing from the
# environment and calls no external command.

# gc_quote VALUE: VALUE as a double-quoted value of a Git configuration file.
gc_quote() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}
  s=${s//$'\t'/\\t}
  printf '"%s"' "$s"
}

# gc_unquote TEXT: the value of a Git configuration line as written by gc_quote (or
# unquoted, without escapes).
gc_unquote() {
  local s=$1 out="" c i
  case "$s" in
  \"*\") s=${s:1:${#s}-2} ;;
  esac
  for ((i = 0; i < ${#s}; i++)); do
    c=${s:i:1}
    if [ "$c" = "\\" ] && ((i + 1 < ${#s})); then
      i=$((i + 1))
      c=${s:i:1}
      case "$c" in
      n) c=$'\n' ;;
      t) c=$'\t' ;;
      esac
    fi
    out+=$c
  done
  printf '%s' "$out"
}

# gc_header SECTION [SUBSECTION]: the section line of a Git configuration file.
gc_header() {
  if [ $# -lt 2 ]; then
    printf '[%s]' "$1"
    return 0
  fi
  local sub=${2//\\/\\\\}
  sub=${sub//\"/\\\"}
  printf '[%s "%s"]' "$1" "$sub"
}

# gc_split KEY: splits a config key (section.variable or section.subsection.variable)
# into three lines: section, subsection (may be empty), variable. Returns 2 for a key
# Git does not accept.
gc_split() {
  local key=$1 section variable sub=""
  case "$key" in
  *.*) ;;
  *) return 2 ;;
  esac
  section=${key%%.*}
  variable=${key##*.}
  if [ "$section.$variable" != "$key" ]; then
    sub=${key#*.}
    sub=${sub%.*}
    [ -n "$sub" ] || return 2
  fi
  [[ $section =~ ^[A-Za-z0-9-]+$ ]] || return 2
  [[ $variable =~ ^[A-Za-z][A-Za-z0-9-]*$ ]] || return 2
  [[ $sub != *$'\n'* ]] || return 2
  printf '%s\n%s\n%s\n' "$section" "$sub" "$variable"
}

# gc_entry KEY VALUE [PREVIOUS_HEADER]: the lines for one key, with its section line
# unless it equals PREVIOUS_HEADER (see gc_section_line), so keys of one section stay
# together. Returns 2 for invalid keys.
gc_entry() {
  local parts section sub variable header
  parts=$(gc_split "$1") || return 2
  section=${parts%%$'\n'*}
  variable=${parts##*$'\n'}
  sub=${parts#*$'\n'}
  sub=${sub%$'\n'*}
  if [ -n "$sub" ]; then
    header=$(gc_header "$section" "$sub")
  else
    header=$(gc_header "$section")
  fi
  [ "$header" = "${3:-}" ] || printf '%s\n' "$header"
  printf '\t%s = %s\n' "$variable" "$(gc_quote "$2")"
}

# gc_section_line KEY: the section line a key belongs to (for gc_entry).
gc_section_line() {
  local parts section sub
  parts=$(gc_split "$1") || return 2
  section=${parts%%$'\n'*}
  sub=${parts#*$'\n'}
  sub=${sub%$'\n'*}
  if [ -n "$sub" ]; then gc_header "$section" "$sub"; else gc_header "$section"; fi
}

# origin_of URL: scheme://host[:port] of an https address, without user info or path.
# Returns 2 for anything but https with a host.
origin_of() {
  local u=$1 rest hostport
  case "$u" in
  https://*) rest=${u#https://} ;;
  *) return 2 ;;
  esac
  hostport=${rest%%[/?#]*}
  hostport=${hostport##*@}
  [ -n "$hostport" ] || return 2
  [[ $hostport =~ ^[][A-Za-z0-9._:-]+$ ]] || return 2
  printf 'https://%s' "${hostport,,}"
}

# shell_quote TEXT: TEXT in single quotes for a shell command line.
shell_quote() {
  local s=${1//\'/\'\\\'\'}
  printf "'%s'" "$s"
}

# json_str TEXT: TEXT as a JSON string.
json_str() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}
  s=${s//$'\r'/\\r}
  s=${s//$'\t'/\\t}
  printf '"%s"' "$s"
}

# json_pointer_token TEXT: TEXT escaped as one token of a JSON Pointer.
json_pointer_token() {
  local s=${1//\~/\~0}
  printf '%s' "${s//\//\~1}"
}
