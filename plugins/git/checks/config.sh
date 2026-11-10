#!/usr/bin/env bash
# Check: Git identity, whether the values of the setting defaults are in effect, and the
# hosts that have a credential through DevBox. Reads only; prints one JSON object.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

status=ok
hints=()
problems=()

if [ -e "$GITCONFIG" ] && [ ! -r "$GITCONFIG" ]; then
  printf '{"status":"error","summary":%s,"data":{"file":%s},"details":[{"label":"File","value":%s}]}\n' \
    "$(json_str "cannot read $GITCONFIG")" "$(json_str "$GITCONFIG")" "$(json_str "$GITCONFIG")"
  exit 0
fi

# --- identity ---
name=$(git_get user.name) || name=""
email=$(git_get user.email) || email=""
if [ -z "$name" ] || [ -z "$email" ]; then
  status=warn
  problems+=("identity incomplete")
  hints+=("{\"text\":\"Set the name and e-mail address for commits\",\"action\":\"git.identity\"}")
fi

# --- defaults ---
defaults=""
applied=0
total=0
while IFS= read -r k; do
  [ -n "$k" ] || continue
  total=$((total + 1))
  want=$(sdk json-get "$DEVBOX_REQUEST" "/settings/defaults/$(json_pointer_token "$k")")
  have=$(git_get "$k") || have=""
  ok=false
  if [ "$have" = "$want" ]; then
    ok=true
    applied=$((applied + 1))
  fi
  defaults+="${defaults:+,}{\"key\":$(json_str "$k"),\"value\":$(json_str "$want"),\"applied\":$ok}"
done < <(sdk json-keys "$DEVBOX_REQUEST" /settings/defaults 2>/dev/null || true)
if [ "$applied" -lt "$total" ]; then
  status=warn
  problems+=("$((total - applied)) of $total defaults not in effect")
  hints+=("{\"text\":\"Apply the Git defaults\",\"action\":\"git.defaults\"}")
fi

# --- credentials: one block per git-host instance ---
hosts=""
host_names=""
instances=()
if [ -f "$GITCONFIG" ]; then
  while IFS= read -r line; do
    case "$line" in
    "# >>> devbox:git:credential:"*" >>>")
      line=${line#"# >>> devbox:git:credential:"}
      instances+=("${line%" >>>"}")
      ;;
    esac
  done <"$GITCONFIG"
fi
for inst in "${instances[@]}"; do
  block="git:credential:$inst"
  origin=$(block_origin "$GITCONFIG" "$block")
  user=$(block_value "$GITCONFIG" "$block" username)
  spec=$(block_value "$GITCONFIG" "$block" helper)
  stored=null
  if file=$(store_file "$spec"); then
    # the store has one URL per line (https://user:token@host); only the host is read
    stored=false
    if [ -r "$file" ]; then
      while IFS= read -r entry || [ -n "$entry" ]; do
        rest=${entry#https://}
        [ "$rest" != "$entry" ] || continue
        rest=${rest%%/*}
        rest=${rest##*@}
        rest=${rest//%3[aA]/:}
        if [ "https://$rest" = "$origin" ]; then
          stored=true
          break
        fi
      done <"$file"
    fi
    if [ "$stored" = false ]; then
      status=warn
      problems+=("no token for $origin")
      hints+=("{\"text\":$(json_str "The token for $origin is missing; apply the credential $inst again")}")
    fi
  fi
  hosts+="${hosts:+,}{\"instance\":$(json_str "$inst"),\"url\":$(json_str "$origin"),\"username\":$(json_str "$user"),\"helper\":$(json_str "$spec"),\"stored\":$stored}"
  host_names+="${host_names:+, }${origin#https://}"
done

if [ -n "$name" ] || [ -n "$email" ]; then
  summary="${name:-?} <${email:-?}>"
else
  summary="No identity"
fi
[ "$total" -eq 0 ] || summary+=", $applied/$total defaults"
[ -z "$host_names" ] || summary+=", hosts: $host_names"
details=""
if [ "${#problems[@]}" -gt 0 ]; then
  joined=""
  for p in "${problems[@]}"; do joined+="${joined:+; }$p"; done
  details="{\"label\":\"Problems\",\"value\":$(json_str "$joined")},"
fi
details+="{\"label\":\"File\",\"value\":$(json_str "$GITCONFIG")}"
hint_json=""
if [ "${#hints[@]}" -gt 0 ]; then
  hint_json=",\"hints\":[$(
    IFS=,
    printf '%s' "${hints[*]}"
  )]"
fi

printf '{"status":%s,"summary":%s,"data":{"identity":{"name":%s,"email":%s},"defaults":[%s],"hosts":[%s]},"details":[%s]%s}\n' \
  "$(json_str "$status")" "$(json_str "$summary")" "$(json_str "$name")" "$(json_str "$email")" \
  "$defaults" "$hosts" "$details" "$hint_json"
