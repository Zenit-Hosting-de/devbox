#!/usr/bin/env bash
# Check: is xrdp installed, are its services enabled and running, and do the values in
# xrdp.ini and sesman.ini match the settings? Reads only; nothing needs root.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"
# shellcheck source=../lib/ini.sh
. "$DEVBOX_PLUGIN_DIR/lib/ini.sh"

pm=$(package_manager)
if [ -z "$pm" ]; then
  printf '{"status":"error","summary":%s,"data":{"installed":false,"manager":null,"missing":[]}}\n' "$(json_str "$no_manager_text")"
  exit 0
fi
mapfile -t wanted < <(server_packages "$pm")
mapfile -t missing < <(missing_of "$pm" "${wanted[@]}")
if [ "${#missing[@]}" -gt 0 ]; then
  list=$(IFS=,; echo "${missing[*]}")
  printf '{"status":"missing","summary":%s,"data":{"installed":false,"manager":%s,"missing":%s},"hints":[{"text":"Install xrdp","action":"xrdp.install"}]}\n' \
    "$(json_str "Not installed: ${list//,/, }")" "$(json_str "$pm")" "$(json_list "${missing[@]}")"
  exit 0
fi

xrdp_ini=$(sdk setting xrdpIni)
sesman_ini=$(sdk setting sesmanIni)
desktop=$(sdk setting desktop)
session_file=$(sdk setting sessionFile)

differences=()
unreadable=()
# compare NAME FILE SECTION KEY WANTED: sets actual_NAME and notes a difference.
compare() {
  local name=$1 file=$2 value rc=0
  value=$(ini_get "$file" "$3" "$4") || rc=$?
  case "$rc" in
  0) [ "$value" = "$5" ] || differences+=("$4") ;;
  1)
    value=""
    differences+=("$4")
    ;;
  *)
    value=""
    case " ${unreadable[*]} " in *" $file "*) ;; *) unreadable+=("$file") ;; esac
    ;;
  esac
  printf -v "actual_$name" '%s' "$value"
}
actual_port="" actual_security="" actual_bpp="" actual_root=""
compare port "$xrdp_ini" Globals port "$(sdk setting port)"
compare security "$xrdp_ini" Globals security_layer "$(sdk setting securityLayer)"
compare bpp "$xrdp_ini" Globals max_bpp "$(sdk setting maxBpp)"
compare root "$sesman_ini" Security AllowRootLogin "$(sdk setting allowRootLogin)"
if [ "$desktop" = xfce ] && ! grep -qxF '# >>> devbox:xrdp >>>' "$session_file" 2>/dev/null; then
  differences+=("session start")
fi

mapfile -t units < <(services)
active=true
enabled=true
service_text=""
for unit in "${units[@]}"; do
  a=inactive
  e=disabled
  if unit_active "$unit"; then a=active; else active=false; fi
  if unit_enabled "$unit"; then e=enabled; else enabled=false; fi
  service_text+="${service_text:+, }$unit: $a, $e"
done

port_json=null
[[ $actual_port =~ ^[0-9]{1,5}$ ]] && port_json=$((10#$actual_port))

status=ok
summary="xrdp is running on port $actual_port"
hints=""
if [ "$active" = false ] && [ "$enabled" = false ]; then
  status=warn
  summary="xrdp is switched off"
  hints='{"text":"Switch xrdp on","action":"xrdp.enable"}'
elif [ "$active" = false ] || [ "$enabled" = false ]; then
  status=warn
  summary="xrdp is not running or not started with the machine"
  hints='{"text":"Switch xrdp on","action":"xrdp.enable"}'
fi
if [ "${#unreadable[@]}" -gt 0 ]; then
  status=warn
  summary="Cannot read ${unreadable[*]}"
elif [ "${#differences[@]}" -gt 0 ]; then
  status=warn
  list=$(IFS=,; echo "${differences[*]}")
  summary="Configuration differs from the settings: ${list//,/, }"
  hints+="${hints:+,}"'{"text":"Apply the configuration","action":"xrdp.configure"}'
fi

printf '{"status":%s,"summary":%s,' "$(json_str "$status")" "$(json_str "$summary")"
printf '"data":{"installed":true,"manager":%s,"missing":[],"active":%s,"enabled":%s,"port":%s,' \
  "$(json_str "$pm")" "$active" "$enabled" "$port_json"
printf '"config":{"port":%s,"securityLayer":%s,"maxBpp":%s,"allowRootLogin":%s},"differences":%s},' \
  "$(json_str "$actual_port")" "$(json_str "$actual_security")" "$(json_str "$actual_bpp")" \
  "$(json_str "$actual_root")" "$(json_list "${differences[@]}")"
printf '"details":[{"label":"Port","value":%s},{"label":"Security layer","value":%s},{"label":"Colour depth","value":%s},{"label":"Services","value":%s},{"label":"Desktop","value":%s}]' \
  "$(json_str "$actual_port")" "$(json_str "$actual_security")" "$(json_str "$actual_bpp")" \
  "$(json_str "$service_text")" "$(json_str "$desktop")"
[ -z "$hints" ] || printf ',"hints":[%s]' "$hints"
printf '}\n'
