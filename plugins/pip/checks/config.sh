#!/usr/bin/env bash
# Check: effective package index of pip and uv. The environment file wins over the
# configuration files, as in the tools. Credentials in addresses are never shown.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

default=$(sdk setting defaultIndex)

for f in "$PIP_CONF" "$UV_CONF" "$ENV_FILE"; do
  if [ -e "$f" ] && [ ! -r "$f" ]; then
    # An unreadable file hides the real index: it is unknown, not the default.
    printf '{"status":"warn","summary":%s,"data":{"mode":%s,"tools":[]}}\n' \
      "$(json_str "cannot read $f")" "$(json_str "$MODE")"
    exit 0
  fi
done

# env_value NAME: the value the environment file exports for NAME (last one wins).
env_value() {
  local line value=""
  [ -f "$ENV_FILE" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    line=$(trim "$line")
    line=${line#export }
    line=$(trim "$line")
    [[ $line == "$1="* ]] || continue
    value=$(shell_unquote "${line#"$1="}")
  done <"$ENV_FILE"
  [ -z "$value" ] || strip_userinfo "$value"
}

# pip_value: index-url of the [global] section of pip.conf (last one wins).
pip_value() {
  local line t section="" value=""
  [ -f "$PIP_CONF" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    t=$(trim "${line%$'\r'}")
    case "$t" in
    '' | '#'* | ';'*) continue ;;
    \[*\]) section=$t ;;
    index-url*=* | index_url*=*)
      [ "$section" != "[global]" ] || value=$(trim "${t#*=}")
      ;;
    esac
  done <"$PIP_CONF"
  printf '%s' "$value"
}

# uv_value: url of the first [[index]] in uv.toml marked as default.
uv_value() {
  local line t in_index=false url="" is_default=false
  [ -f "$UV_CONF" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    t=$(trim "${line%$'\r'}")
    case "$t" in
    '' | '#'*) continue ;;
    \[*)
      if $in_index && $is_default && [ -n "$url" ]; then break; fi
      in_index=false url="" is_default=false
      [ "$t" != "[[index]]" ] || in_index=true
      ;;
    url*=*) ! $in_index || url=$(trim "${t#*=}") ;;
    default*=*) ! $in_index || [ "$(trim "${t#*=}")" != true ] || is_default=true ;;
    esac
  done <"$UV_CONF"
  if $in_index && $is_default && [ -n "$url" ]; then
    url=${url#\"}
    url=${url%\"}
    printf '%s' "$url"
  fi
}

tools=""
summary=""
details=""
for tool in "${TARGETS[@]}"; do
  case "$tool" in
  pip)
    var=PIP_INDEX_URL file=$PIP_CONF
    value=$(env_value PIP_INDEX_URL)
    [ -n "$value" ] || value=$(pip_value)
    ;;
  uv)
    var=UV_DEFAULT_INDEX file=$UV_CONF
    value=$(env_value UV_DEFAULT_INDEX)
    [ -n "$value" ] || value=$(uv_value)
    ;;
  *) continue ;;
  esac
  configured=true
  if [ -z "$value" ]; then
    configured=false
    value=$default
  fi
  tools+="${tools:+,}{\"tool\":$(json_str "$tool"),\"index\":$(json_str "$value"),\"configured\":$configured}"
  summary+="${summary:+, }$tool: $value"
  details+="${details:+,}{\"label\":$(json_str "$tool"),\"value\":$(json_str "$file ($var)")}"
done

[ -n "$summary" ] || summary="No tool selected in the setting targets"
printf '{"status":"ok","summary":%s,"data":{"mode":%s,"tools":[%s]},"details":[%s]}\n' \
  "$(json_str "$summary")" "$(json_str "$MODE")" "$tools" "$details"
