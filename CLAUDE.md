# DevBox – working agreement for Claude Code

DevBox sets up and maintains Linux developer machines. One Go binary (`devbox`)
with an embedded Angular UI; every capability is a plugin (`plugins/<id>/plugin.json`).

## Golden rules
- Nothing is hard-coded: no hostnames, URLs, registry names, versions or paths in code.
  Every value is a plugin setting with a default in plugin.json. Defaults point to public
  sources or are empty. Use example.com in tests, docs and fixtures.
- JSON everywhere. Every file DevBox reads or writes has a schema in `schema/*.v1.json`.
  Schemas are the source of truth; never edit `internal/gen/` or `web/src/app/gen/` by hand.
  Allowed non-JSON files: .github/workflows/*.yml, .goreleaser.yaml, eslint.config.mjs,
  go.mod, Makefile. Do not add others.
- Strict parsing: unknown keys are errors (except `$schema`, `$comment`).
- The core knows no language or tool. If a plugin needs something new, extend the protocol.
- Idempotent actions, dry-run support (`DEVBOX_DRY_RUN=1`), backups before file changes,
  marked blocks (`# >>> devbox:<plugin> >>>`) instead of appends.
- Secrets only via the request file; never in env vars, argv, logs or events.
- UI and CLI texts are neutral (no du/Sie/ich) and exist in de.json and en.json.

## Commands
- make generate   # types from schema/ and api/openapi.json
- make dev        # core + ng serve
- make lint test  # must pass before every commit
- make e2e        # Playwright in a test container

## Layout
cmd/devbox, internal/{api,catalog,cli,config,history,platform,plugin,profile,runner,
schema,sdk,tui,vault,gen}, schema/, api/openapi.json, plugins/, web/, sdk/ts/,
templates/plugin/, packaging/, test/, docs/

## Go
Go 1.23+, CGO_ENABLED=0, log/slog, context everywhere, wrap errors with %w,
no panics outside main, table-driven tests, testscript for CLI flows.

## Web
Angular standalone + signals + OnPush, zoneless, zenit-ui components only,
no `any`, one WebSocket via the Sessions store, forms via SchemaForm.

## Plugins
Bash with `set -euo pipefail`, shellcheck clean, only `devbox sdk` and commands
listed in requires.commands. Checks print one JSON object to stdout. Actions write
JSON Lines to fd 3. Each plugin has bats tests; `devbox plugin test` must pass.

## Commits
Conventional Commits, DCO sign-off (`git commit -s`), one work package per PR.
