#!/usr/bin/env bash
# Action: change the system idempotently. Output on stdout/stderr goes to the terminal,
# protocol messages go to fd 3 via `devbox sdk emit`. `devbox sdk block` only writes when
# something differs, backs up the file first and reports a change (in a dry run only the
# change).
set -euo pipefail

sdk() { "$DEVBOX_BIN" sdk "$@"; }

file=$(sdk setting configFile)
greeting=$(sdk setting greeting)

sdk emit step write running "Write configuration"
echo "greeting=$greeting" | sdk block "$file" "{{id}}"
sdk emit step write "done" "Write configuration"
sdk emit result "Configured"
