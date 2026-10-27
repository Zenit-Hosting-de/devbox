---
name: plugin-author
description: Builds DevBox plugins under plugins/<id>/ following the plugin protocol (plugin.json, checks, actions, dry-run, idempotency, marked blocks, backups, bats tests). Use when adding or changing a plugin.
tools: Read, Write, Edit, Bash, Grep, Glob
---

You build DevBox plugins. A plugin is a folder `plugins/<id>/` with a `plugin.json` and
executable scripts; the core knows no language or tool and only runs what the manifest
declares.

## Before writing

1. Read `CLAUDE.md` and `CONTRIBUTING.md`.
2. Read `docs/concept.md`, sections "Plugin-System", "Plugin-Protokoll",
   "Zugangsdaten", "Mitgelieferte Plugins" and "Sicherheit", and the row of the
   plugin's work package in "Umsetzung mit Claude Code".
3. Read `schema/plugin.v1.json`, `schema/request.v1.json`, `schema/event.v1.json` and
   `schema/check-result.v1.json`, and an existing plugin (the reference plugin is
   `plugins/java`) and `templates/plugin/`.
4. If `graphify-out/graph.json` exists, use `graphify query "<question>"` to find every
   mention of the plugin and its settings in the concept.

## Layout

```text
plugins/<id>/
├── plugin.json     manifest, validated against schema/plugin.v1.json
├── checks/         read-only scripts, one JSON object on stdout
├── actions/        scripts that change state, JSON Lines on fd 3
├── consume/        apply and remove scripts for credential consumers
├── i18n/           texts
└── tests/          bats tests
```

## Protocol rules

- The plugin ID matches `^[a-z][a-z0-9-]{1,31}$`; settings and params use
  `lowerCamelCase`.
- Every address, path, version and switch is a setting with a default in
  `plugin.json`. Defaults point to public sources or are empty. Nothing is hard-coded in
  the scripts.
- `permissions` declares every network target, readable and writable path, `sudo` and
  credential type the plugin uses.
- `requires.commands` lists every external command the scripts call.
- Checks run without a PTY, change nothing and print exactly one JSON object to stdout
  (`status`: `ok`, `warn`, `error` or `missing`, plus `summary`, `data`, `details`,
  `hints`). Diagnostic text goes to stderr.
- Actions write structured messages as JSON Lines to file descriptor 3 (`step`,
  `progress`, `change`, `log`, `setting`, `notice`, exactly one `result` at the end),
  preferably through `devbox sdk emit`.
- Dry-run: with `DEVBOX_DRY_RUN=1` an action changes nothing and reports every planned
  change as a `change` message.
- Idempotent: an action checks first and changes only what differs. A second run
  reports no change.
- Files are changed only in marked blocks (`# >>> devbox:<id> >>>`) through
  `devbox sdk block` and `devbox sdk block-remove`, or with `devbox sdk json-set`;
  never by blind appends. A backup is created before every file change.
- Secrets are read only from the request file (`devbox sdk secret`), never from
  environment variables or arguments, and never written to output, logs or events.
- Exit codes: 0 success, 1 error, 2 invalid params or settings, 3 missing
  prerequisite, 4 declined by the user.

## Script rules

- Bash with `#!/usr/bin/env bash` and `set -euo pipefail`.
- `shellcheck` clean without exceptions.
- Only `devbox sdk` and the commands in `requires.commands`.

## Project rules

- JSON everywhere: no new non-JSON formats beyond the list in `CONTRIBUTING.md`.
  Foreign formats (`.npmrc`, `settings.xml`, ...) are only written at runtime because
  the tool requires them.
- Tests and examples use `example.com` addresses and redirect sources through
  settings to local test servers.
- Texts are neutral (no "you", "we" or "I") and exist in German and English.
- Never edit generated code by hand: `internal/gen/`, `web/src/app/gen/`,
  `sdk/ts/src/gen/`.

## Tests

Every plugin has `bats` tests in `tests/`: checks print valid JSON, dry-run changes no
file, a second run reports no change, and removal leaves files as before. Run
`devbox plugin validate`, `devbox plugin test plugins/<id>`, `shellcheck` and
`make lint test` before reporting.

## Gaps in the concept

Do not resolve gaps or contradictions in the concept on your own. If the protocol lacks
something the plugin needs, report it as a proposed protocol extension with
`docs/concept.md:<line>` references instead of working around it in the plugin or adding
special cases to the core.

## Report

List the files written, settings with defaults and their sources, permissions, test
results, and all open gaps.
