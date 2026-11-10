#!/usr/bin/env bash
# INI helper of the xrdp plugin: reads and sets single keys of xrdp.ini and sesman.ini
# and leaves every other byte of the file as it is. Pure Bash, no external command.
#
# The format, as xrdp reads it:
#   - "[Section]" starts a section; lines before the first one belong to the section "".
#   - "key=value" sets a key; the value is the rest of the line.
#   - Lines that start with ";" or "#" are comments; there are no inline comments.
#   - Section and key names are compared without regard to case.
#   - The same key name may occur in several sections (port in [Globals] and [Xorg]).
#
# Line endings (LF or CRLF), indentation, the spacing around "=", comments, blank lines
# and a missing newline at the end of the file are kept.

# _ini_trim TEXT: TEXT without leading and trailing blanks, in INI_TRIMMED.
_ini_trim() {
  INI_TRIMMED=$1
  INI_TRIMMED=${INI_TRIMMED#"${INI_TRIMMED%%[![:space:]]*}"}
  INI_TRIMMED=${INI_TRIMMED%"${INI_TRIMMED##*[![:space:]]}"}
}

# _ini_parse LINE: classifies a line (without its line ending).
#   INI_KIND    section | key | other
#   INI_NAME    section or key name, lower case
#   INI_PREFIX  for a key: everything up to the value (indentation, key, "=", blanks)
#   INI_VALUE   for a key: the value without trailing blanks
_ini_parse() {
  local line=$1
  INI_KIND=other
  INI_NAME=""
  INI_PREFIX=""
  INI_VALUE=""
  if [[ $line =~ ^[[:space:]]*\[([^]]*)\][[:space:]]*$ ]]; then
    INI_KIND=section
    _ini_trim "${BASH_REMATCH[1]}"
    INI_NAME=${INI_TRIMMED,,}
    return 0
  fi
  [[ $line =~ ^[[:space:]]*[\;#] ]] && return 0
  if [[ $line =~ ^([[:space:]]*([^=]*[^=[:space:]])[[:space:]]*=[[:space:]]*)(.*)$ ]]; then
    INI_KIND=key
    INI_PREFIX=${BASH_REMATCH[1]}
    INI_VALUE=${BASH_REMATCH[3]}
    _ini_trim "${BASH_REMATCH[2]}"
    INI_NAME=${INI_TRIMMED,,}
    _ini_trim "$INI_VALUE"
    INI_VALUE=$INI_TRIMMED
  fi
  return 0
}

# _ini_valid SECTION KEY [VALUE]: names and value fit into one INI line.
_ini_valid() {
  case "$1" in *$'\n'* | *$'\r'* | *'['* | *']'*) return 1 ;; esac
  case "$2" in "" | *$'\n'* | *$'\r'* | *=* | '['* | ';'* | '#'* | ' '* | *' ') return 1 ;; esac
  case "${3:-}" in *$'\n'* | *$'\r'*) return 1 ;; esac
  return 0
}

# ini_get FILE SECTION KEY: prints the value of KEY in SECTION. With several lines for
# the key the last one counts, as in xrdp. Returns 1 when the key is not set, 2 for
# invalid names or an unreadable file.
ini_get() {
  local LC_ALL=C
  local file=$1 section=${2,,} key=${3,,} line current="" found=1 value=""
  _ini_valid "$2" "$3" || return 2
  [ -r "$file" ] || return 2
  while IFS= read -r line || [ -n "$line" ]; do
    _ini_parse "${line%$'\r'}"
    case "$INI_KIND" in
    section) current=$INI_NAME ;;
    key)
      if [ "$current" = "$section" ] && [ "$INI_NAME" = "$key" ]; then
        value=$INI_VALUE
        found=0
      fi
      ;;
    esac
  done <"$file"
  [ "$found" -eq 0 ] || return 1
  printf '%s\n' "$value"
}

# ini_set FILE SECTION KEY VALUE: prints the content of FILE with KEY in SECTION set to
# VALUE; FILE itself is not changed. Rules:
#   - Every line that sets the key in the section gets the value; a line that has it
#     already stays untouched, so an unchanged file comes out byte for byte.
#   - A key that is not set is added after its commented-out line (";key=…" or
#     "#key=…") in the section, else after the last line of the section.
#   - A missing section is added at the end of the file.
#   - Keys of the same name in other sections are never touched.
# Returns 2 for invalid names or values, or an unreadable file.
ini_set() {
  local LC_ALL=C
  local file=$1 section=${2,,} key=${3,,} value=$4
  local -a lines=()
  local line body cr eol=$'\n' final_newline=1 current="" i
  local done_set=0 section_seen=0 last_comment=-1 last_content=-1 insert_at
  _ini_valid "$2" "$3" "$4" || return 2
  [ -r "$file" ] || return 2

  while IFS= read -r line; do lines+=("$line"); done <"$file"
  if [ -n "$line" ]; then
    lines+=("$line")
    final_newline=0
  fi
  # new lines end like the first line of the file
  [ "${#lines[@]}" -gt 0 ] && [[ ${lines[0]} == *$'\r' ]] && eol=$'\r\n'

  [ -z "$section" ] && section_seen=1
  for i in "${!lines[@]}"; do
    line=${lines[$i]}
    body=${line%$'\r'}
    cr=${line#"$body"}
    _ini_parse "$body"
    if [ "$INI_KIND" = section ]; then
      current=$INI_NAME
      if [ "$current" = "$section" ]; then
        section_seen=1
        last_content=$i
      fi
      continue
    fi
    [ "$current" = "$section" ] || continue
    [[ $body =~ ^[[:space:]]*$ ]] || last_content=$i
    if [ "$INI_KIND" = key ] && [ "$INI_NAME" = "$key" ]; then
      done_set=1
      [ "$INI_VALUE" = "$value" ] || lines[i]="$INI_PREFIX$value$cr"
    elif [[ $body =~ ^[[:space:]]*[\;#]+[[:space:]]*([^=]*[^=[:space:]])[[:space:]]*= ]] &&
      [ "${BASH_REMATCH[1],,}" = "$key" ]; then
      last_comment=$i
    fi
  done

  if [ "$done_set" -eq 0 ]; then
    if [ "$section_seen" -eq 1 ]; then
      insert_at=$last_content
      [ "$last_comment" -ge 0 ] && insert_at=$last_comment
      # the last line of the file had no newline: it gets one, the new line ends the file
      if [ "$final_newline" -eq 0 ] && [ "$insert_at" -eq $((${#lines[@]} - 1)) ]; then
        lines[insert_at]+=${eol%$'\n'}
        final_newline=1
      fi
      lines=("${lines[@]:0:$((insert_at + 1))}" "$3=$value${eol%$'\n'}" "${lines[@]:$((insert_at + 1))}")
    else
      if [ "${#lines[@]}" -gt 0 ]; then
        [ "$final_newline" -eq 1 ] || lines[-1]+=${eol%$'\n'}
        [[ ${lines[-1]} =~ ^[[:space:]]*$ ]] || lines+=("${eol%$'\n'}")
      fi
      lines+=("[$2]${eol%$'\n'}" "$3=$value${eol%$'\n'}")
      final_newline=1
    fi
  fi

  for i in "${!lines[@]}"; do
    if [ "$i" -eq $((${#lines[@]} - 1)) ] && [ "$final_newline" -eq 0 ]; then
      printf '%s' "${lines[$i]}"
    else
      printf '%s\n' "${lines[$i]}"
    fi
  done
}
