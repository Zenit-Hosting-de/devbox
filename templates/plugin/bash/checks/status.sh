#!/usr/bin/env bash
# Check: report the state as one JSON object on stdout. Read only, fast, no network.
set -euo pipefail

file=$("$DEVBOX_BIN" sdk setting configFile)
if grep -q '>>> devbox:{{id}} >>>' "$file" 2>/dev/null; then
  echo '{"status":"ok","summary":"Configured"}'
else
  echo '{"status":"missing","summary":"Not configured","hints":[{"text":"Apply the configuration","action":"{{id}}.apply"}]}'
fi
