# Contributing to DevBox

Contributions are welcome: bug reports, feature requests, plugins, documentation and
code. All participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md).
Security issues are reported privately as described in [SECURITY.md](SECURITY.md).

The concept in [docs/concept.md](docs/concept.md) (German) is the reference for all
technical decisions. The working rules for code changes are summarised in
[CLAUDE.md](CLAUDE.md) and below.

## Local setup

Required on the system:

- Go 1.26 or newer (the `go` line in go.mod is authoritative; the pinned golangci-lint tool requires it)
- Node.js 22 or newer with npm
- `shellcheck`
- `bats`

Everything else (linters, code generators, formatters) is pinned in the project: Go
tools through the `tool` directive in `go.mod`, npm tools in `devDependencies`.

```sh
npm ci
make generate
make lint test
```

## Make targets

| Target | Purpose |
| --- | --- |
| `make generate` | generate Go and TypeScript types from `schema/` and `api/openapi.json` |
| `make build` | build all npm workspaces and the `devbox` binary |
| `make dev` | run the core for local development |
| `make test` | run all tests |
| `make lint` | run all linters and validate all JSON files against their schemas |
| `make e2e` | run end-to-end tests |
| `make snapshot` | local GoReleaser snapshot build |

`make lint test` must pass before every commit.

## Workflow

- One work package (for example `AP-03`) is one branch and one pull request.
- A pull request needs green checks and a review from the [CODEOWNERS](.github/CODEOWNERS).
- The pull request description lists the acceptance criteria of the work package as a
  checklist, each item backed by a test.
- Order of work: schema and OpenAPI first, then `make generate`, then tests, then code.
- A technical decision from the concept is changed only through an architecture
  decision record (ADR) under [docs/adr/](docs/adr/), written and accepted before the
  change.

## Developer Certificate of Origin (DCO)

DevBox uses the [Developer Certificate of Origin](https://developercertificate.org/)
instead of a contributor license agreement. The DCO is a statement that the
contributor wrote the change or otherwise has the right to submit it under the
project's open source license (Apache-2.0). Contributors keep the copyright of their
changes.

Every commit carries a `Signed-off-by` line with the author's name and email address,
added by:

```sh
git commit -s
```

The resulting line looks like `Signed-off-by: Jane Doe <jane.doe@example.com>` and must
match the commit author. A CI check rejects pull requests with commits that are not
signed off. Missing sign-offs on existing commits are added with
`git rebase --signoff main`.

## Commit messages

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/),
checked by commitlint. The changelog and the version number are derived from them.

```text
<type>(<scope>): <summary>

<body>

Signed-off-by: Jane Doe <jane.doe@example.com>
```

Types:

| Type | Use |
| --- | --- |
| `feat` | new feature |
| `fix` | bug fix |
| `docs` | documentation only |
| `test` | tests only |
| `refactor` | code change without new behaviour or fix |
| `perf` | performance improvement |
| `build` | build system, dependencies, packaging |
| `ci` | CI configuration |
| `chore` | maintenance without effect on the product |
| `revert` | revert of an earlier commit |

Scopes name the affected part, for example `schema`, `api`, `cli`, `config`, `runner`,
`vault`, `web`, `sdk`, `plugin/java`, `packaging`.

Breaking changes are marked with `!` after the type or scope and a
`BREAKING CHANGE:` footer that describes the change and the migration:

```text
feat(schema)!: rename plugins.paths to plugins.searchPaths

BREAKING CHANGE: profiles must use plugins.searchPaths.
```

Within a major version of the schemas and the plugin protocol, only additions are
allowed (see [GOVERNANCE.md](GOVERNANCE.md)).

## Rules for all changes

### JSON everywhere

Every file DevBox reads or writes is JSON or JSON Lines and has a schema in
`schema/*.v1.json`. Tool configuration is JSON as well (`package.json`,
`tsconfig.json`, `.golangci.json`, `.prettierrc.json`, `renovate.json`,
`lefthook.json`, `.commitlintrc.json`, release-please files). JSON files use two-space
indentation, end with a newline and carry `"$schema"`.

Allowed non-JSON files, because a tool requires the format or the file is source code
or documentation:

- `.github/workflows/*.yml`
- `.goreleaser.yaml`
- `eslint.config.mjs`
- `go.mod`, `go.sum`
- `Makefile`
- `.gitignore`, `.gitattributes`
- `.github/CODEOWNERS`
- `LICENSE`, `NOTICE`
- Markdown (`*.md`), including Markdown with YAML front matter where the tool requires
  it: `.github/ISSUE_TEMPLATE/*.md` and `.claude/agents/*.md`
- source files: `*.go`, `*.ts`, `*.mjs`, `*.sh`, `*.bash`, `*.bats`, `*.txtar`

No other non-JSON formats may be added. In particular no YAML, TOML or INI files
beyond the list above.

Foreign formats (`.npmrc`, `settings.xml`, `pip.conf`, `NuGet.Config`) are written
only by plugins at runtime, because the respective tools require them.

### Generated code

`make generate` creates types from `schema/` and `api/openapi.json`. Run it after
every change to a schema or to the OpenAPI contract and commit the result. CI fails
if `make generate` leaves a diff.

The following directories are generated and never edited by hand:

- `internal/gen/`
- `web/src/app/gen/`
- `sdk/ts/src/gen/`

### Nothing hard-coded

No hostname, URL, registry name, version or path is hard-coded in code. Every value is
a plugin setting with a default in `plugin.json`. Defaults point to public sources or
are empty. `make lint` rejects URL literals outside allowed places.

Tests, documentation and fixtures use `example.com` (and its subdomains) for all
addresses. The only exception is `test/fixtures/valid/concept/`, which holds the
examples of the concept verbatim.

### Strict parsing

Unknown keys are errors, reported with path and line. The only keys allowed in
addition to the schema are `$schema` and `$comment`.

### Texts

User-facing texts of UI and CLI are never written into Go or TypeScript code. They
live only in the language packs `lang-de` and `lang-en` and are always maintained in
both languages at the same time.

Texts are neutral: they do not address anyone and do not speak as "I" or "we"
("Token saved", "Select version").

### Go

`gofumpt`, `golangci-lint` (errcheck, revive, gosec, staticcheck), `log/slog`, a
context passed to every process start, errors wrapped with `%w`, no `panic` outside
`main`, table-driven tests, `testscript` for CLI flows.

### TypeScript

`strict`, no `any`, standalone components, `OnPush`, signals instead of RxJS except
for WebSocket streams.

## Plugin conventions

- Scripts are Bash with `#!/usr/bin/env bash` and `set -euo pipefail`.
- `shellcheck` passes without exceptions.
- Scripts depend only on `devbox sdk` and the commands listed in `requires.commands`.
- Checks print exactly one JSON object to stdout and change nothing.
- Actions write JSON Lines to file descriptor 3, support dry-run
  (`DEVBOX_DRY_RUN=1`, reporting planned changes as `change` messages) and are
  idempotent: a second run reports no change.
- Files are changed only in marked blocks (`# >>> devbox:<plugin> >>>`), never by
  blind appends, and a backup is created before every change.
- Secrets are read only from the request file, never from environment variables or
  arguments, and never written to logs or events.
- Every plugin has `bats` tests, and `devbox plugin test` passes.

New plugins are proposed with the plugin proposal issue template.
