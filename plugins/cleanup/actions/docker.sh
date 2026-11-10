#!/usr/bin/env bash
# Action: docker system prune, only when the setting docker is on and docker is in
# PATH. The dry run reports the reclaimable space from docker system df.
set -euo pipefail
# shellcheck source=../lib/common.sh
. "$DEVBOX_PLUGIN_DIR/lib/common.sh"

if [ "$(sdk setting docker)" != true ]; then
  sdk emit result "Docker cleanup is switched off"
  exit 0
fi
if ! command -v docker >/dev/null 2>&1; then
  sdk emit result "Docker is not installed, nothing to clean"
  exit 0
fi

all=$(sdk setting dockerAllImages)
volumes=$(sdk setting dockerVolumes)

if is_dry_run; then
  if ! usage=$(docker system df --format '{{json .}}'); then
    sdk emit log error "docker system df failed; the Docker daemon may not be running or not be reachable for this user"
    exit 1
  fi
  parts=""
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    kind=$(sdk json-get - /Type <<<"$line")
    reclaimable=$(sdk json-get - /Reclaimable <<<"$line")
    [ "$kind" = "Local Volumes" ] && [ "$volumes" != true ] && continue
    case "$reclaimable" in 0B* | '0 B'* | '') continue ;; esac
    sdk emit change other delete "docker: $kind" "up to $reclaimable reclaimable"
    parts+="${parts:+, }$kind $reclaimable"
  done <<<"$usage"
  if [ -z "$parts" ]; then
    sdk emit result "Nothing to reclaim in Docker"
  else
    sdk emit result "Would reclaim up to: $parts"
  fi
  exit 0
fi

args=(system prune --force)
[ "$all" = true ] && args+=(--all)
[ "$volumes" = true ] && args+=(--volumes)
if ! out=$(docker "${args[@]}" 2>&1); then
  printf '%s\n' "$out" >&2
  sdk emit log error "docker system prune failed; the Docker daemon may not be running or not be reachable for this user"
  exit 1
fi
printf '%s\n' "$out" >&2

reclaimed=""
removed=0
while IFS= read -r line; do
  case "$line" in
  'Total reclaimed space:'*)
    reclaimed=${line#Total reclaimed space:}
    reclaimed=${reclaimed# }
    ;;
  '') ;;
  *) removed=1 ;;
  esac
done <<<"$out"

case "$reclaimed" in '' | 0B | '0 B') reclaimed="" ;; esac
if [ "$removed" = 1 ] || [ -n "$reclaimed" ]; then
  sdk emit change other delete docker "unused data removed, ${reclaimed:-0B} reclaimed"
  sdk emit result "Docker: ${reclaimed:-0B} reclaimed"
else
  sdk emit result "Nothing to reclaim in Docker"
fi
