# DevBox

DevBox sets up Linux machines for software development and keeps them up to date:
runtimes (Node.js, Java, .NET, Python), tools, Git, access to package registries,
Docker, remote desktop and cleanup. It is operated through a web UI, a CLI or a
terminal menu. Teams use it with their own servers, registries and plugins through
profiles, without changing a line in the core.

DevBox is a single Go binary (`devbox`) with an embedded web UI. Every capability is a
plugin (`plugins/<id>/plugin.json`); the core knows no language, registry or server.
All configuration, profiles, manifests, protocol messages and translations are JSON,
with JSON Schema 2020-12 as the source of truth.

DevBox by Zenit-Hosting is not related to _Jetify devbox_, a Nix-based tool for
per-project development environments that also installs a `devbox` command.

## Status

Early development: schemas and scaffold (work packages AP-01 and AP-02). Nothing is
released yet. Installation instructions follow with the first release.

## Getting started

Build from source (Go version in `go.mod`, Node.js with npm), then try the bundled
`base` and `java` plugins:

```bash
make tools build && cp bin/devbox ~/.local/bin/
devbox plugin list
devbox status
devbox run java.install --dry-run
devbox run java.install -p version=21
devbox history
```

[docs/usage.md](docs/usage.md) explains files, settings, dry runs, exit codes, own
plugins and what is not available yet.

## Repository layout

| Path                | Content                                                                                                                                 |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/devbox/`       | entry point of the `devbox` binary                                                                                                      |
| `internal/`         | core packages (API, catalog, CLI, config, history, platform, plugin host, profiles, runner, schema, SDK commands, terminal menu, vault) |
| `internal/gen/`     | Go types generated from `schema/` and `api/openapi.json` (never edited by hand)                                                         |
| `schema/`           | JSON Schemas `*.v1.json`, the source of truth for every file DevBox reads or writes                                                     |
| `api/openapi.json`  | API contract                                                                                                                            |
| `plugins/`          | bundled plugins                                                                                                                         |
| `web/`              | web UI (npm workspace)                                                                                                                  |
| `sdk/ts/`           | TypeScript SDK for plugin authors (`@zenit-hosting/devbox-sdk`)                                                                         |
| `templates/plugin/` | templates for `devbox plugin new`                                                                                                       |
| `packaging/`        | systemd unit, install script, shell completions, distribution packages                                                                  |
| `test/`             | fixtures, CLI flow tests, end-to-end tests, test images                                                                                 |
| `docs/`             | user and developer documentation, architecture decision records (`docs/adr/`)                                                           |
| `.github/`          | workflows, issue and pull request templates, CODEOWNERS                                                                                 |

## Make targets

`make` is the only entry point for building, testing, linting and code generation.

| Target          | Effect                                                                      |
| --------------- | --------------------------------------------------------------------------- |
| `make generate` | generate Go and TypeScript types from `schema/` and `api/openapi.json`      |
| `make build`    | build all npm workspaces and the `devbox` binary                            |
| `make dev`      | run the core for local development                                          |
| `make test`     | Go tests (including schema fixtures), workspace tests, `bats` for plugins   |
| `make lint`     | Go, shell, TypeScript and JSON linters, schema validation of all JSON files |
| `make e2e`      | end-to-end tests (no tests yet)                                             |
| `make snapshot` | local GoReleaser snapshot build                                             |

## Documentation

- [docs/usage.md](docs/usage.md): building and using DevBox today
- [docs/concept.md](docs/concept.md): concept and implementation plan (German, reference document)
- [docs/adr/](docs/adr/): architecture decision records
- [CONTRIBUTING.md](CONTRIBUTING.md): how to contribute
- [SECURITY.md](SECURITY.md): reporting vulnerabilities
- [GOVERNANCE.md](GOVERNANCE.md): decision making and versioning
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md): Contributor Covenant 2.1

## License

Apache License 2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).
Copyright 2026 Zenit-Hosting and DevBox contributors.
