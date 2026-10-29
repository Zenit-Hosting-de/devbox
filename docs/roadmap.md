# DevBox roadmap to 1.0

State as of 2026-10-28. Sources: `docs/concept.md` (sections "Fahrplan", "Arbeitspakete",
"Offene Entscheidungen", "Nächste Schritte"), the open concept gaps collected while planning
AP-01/AP-02, and the pull requests of both repositories. Work packages (AP) and their acceptance
criteria are defined in the concept; this file only tracks order, state and open decisions.

## Status per work package

| AP    | Content                                               | Status                                                                                                                                  | PR                                                                                                                                                                              | Depends on     |
| ----- | ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| AP-01 | Scaffold, CI, release-please                          | merged                                                                                                                                  | [#1](https://github.com/Zenit-Hosting-de/devbox/pull/1), [#3](https://github.com/Zenit-Hosting-de/devbox/pull/3)                                                                | –              |
| AP-02 | Schemas, fixtures, `make generate`                    | merged                                                                                                                                  | [#1](https://github.com/Zenit-Hosting-de/devbox/pull/1)                                                                                                                         | 01             |
| AP-03 | Configuration layers, placeholders, locks             | merged                                                                                                                                  | [#4](https://github.com/Zenit-Hosting-de/devbox/pull/4)                                                                                                                         | 02             |
| AP-04 | Plugin host                                           | merged                                                                                                                                  | [#5](https://github.com/Zenit-Hosting-de/devbox/pull/5)                                                                                                                         | 02, 03         |
| AP-05 | Runner                                                | merged                                                                                                                                  | [#6](https://github.com/Zenit-Hosting-de/devbox/pull/6)                                                                                                                         | 04             |
| AP-06 | History                                               | merged                                                                                                                                  | [#10](https://github.com/Zenit-Hosting-de/devbox/pull/10)                                                                                                                       | 05             |
| AP-07 | Vault                                                 | merged                                                                                                                                  | [#11](https://github.com/Zenit-Hosting-de/devbox/pull/11)                                                                                                                       | 02             |
| AP-08 | `devbox sdk` commands                                 | merged                                                                                                                                  | [#7](https://github.com/Zenit-Hosting-de/devbox/pull/7)                                                                                                                         | 05             |
| AP-09 | REST API                                              | subset merged (PAM login, rate limit, TLS, non-loopback deferred)                                                                       | [#18](https://github.com/Zenit-Hosting-de/devbox/pull/18)                                                                                                                       | 03–07          |
| AP-10 | WebSocket                                             | merged                                                                                                                                  | [#20](https://github.com/Zenit-Hosting-de/devbox/pull/20)                                                                                                                       | 09             |
| AP-11 | CLI                                                   | in progress (status, history, logs, cred, serve done; check, actions, group, setup, profile, doctor, schema print, completion missing)  | [#12](https://github.com/Zenit-Hosting-de/devbox/pull/12), [#18](https://github.com/Zenit-Hosting-de/devbox/pull/18)                                                            | 09             |
| AP-12 | Profiles                                              | not started                                                                                                                             | –                                                                                                                                                                               | 03             |
| AP-13 | `java` reference plugin, plugin tooling               | merged                                                                                                                                  | [#8](https://github.com/Zenit-Hosting-de/devbox/pull/8)                                                                                                                         | 05, 08         |
| AP-14 | UI base                                               | in progress (UI merged; Playwright/axe e2e deferred)                                                                                    | [#19](https://github.com/Zenit-Hosting-de/devbox/pull/19), [#24](https://github.com/Zenit-Hosting-de/devbox/pull/24)                                                            | 10             |
| AP-15 | SchemaForm                                            | subset merged (SchemaForm in the action form)                                                                                           | [#19](https://github.com/Zenit-Hosting-de/devbox/pull/19)                                                                                                                       | 14             |
| AP-16 | Credentials                                           | in progress (CLI done; git-host/ssh-key/generic types, `credential.changed` event, UI pages, vault migration on backend change missing) | [#15](https://github.com/Zenit-Hosting-de/devbox/pull/15), [#16](https://github.com/Zenit-Hosting-de/devbox/pull/16), [#17](https://github.com/Zenit-Hosting-de/devbox/pull/17) | 07, 13, 15     |
| AP-17 | Runtimes `system`, `base`, `node`, `dotnet`, `python` | in progress (`system`, `base`, `node` done; `dotnet`, `python` missing)                                                                 | [#12](https://github.com/Zenit-Hosting-de/devbox/pull/12), [#13](https://github.com/Zenit-Hosting-de/devbox/pull/13), [#14](https://github.com/Zenit-Hosting-de/devbox/pull/14) | 13             |
| AP-18 | Tool plugins (9)                                      | in progress (`npm`, `maven` done; `docker`, `git`, `githost`, `gradle`, `nuget`, `pip` missing)                                         | [#16](https://github.com/Zenit-Hosting-de/devbox/pull/16), [#17](https://github.com/Zenit-Hosting-de/devbox/pull/17)                                                            | 16             |
| AP-19 | `xrdp`, `cleanup`, `templates`                        | not started                                                                                                                             | –                                                                                                                                                                               | 13             |
| AP-20 | First-run setup and groups                            | not started                                                                                                                             | –                                                                                                                                                                               | 17–19          |
| AP-21 | Terminal menu                                         | not started                                                                                                                             | –                                                                                                                                                                               | 11, 15         |
| AP-22 | Migration from DevBox 2                               | not started                                                                                                                             | –                                                                                                                                                                               | 16–19          |
| AP-23 | Catalog                                               | not started (plugins repo scaffold merged)                                                                                              | –                                                                                                                                                                               | 04             |
| AP-24 | Sandbox                                               | not started                                                                                                                             | –                                                                                                                                                                               | 05             |
| AP-25 | Packages, container matrix                            | in progress (GoReleaser tarballs done; deb/rpm/AUR, `install.sh`, package repositories, cosign missing)                                 | [#14](https://github.com/Zenit-Hosting-de/devbox/pull/14), [#21](https://github.com/Zenit-Hosting-de/devbox/pull/21), [#23](https://github.com/Zenit-Hosting-de/devbox/pull/23) | 20             |
| AP-26 | Documentation website                                 | not started                                                                                                                             | –                                                                                                                                                                               | 11, 02         |
| AP-27 | Repositories                                          | in progress (`plugins` kind done in CLI and API; web UI, other kinds, credentials, Sigstore, `repo init` missing)                       | –                                                                                                                                                                               | 04, 12, 17, 23 |
| AP-28 | Language packs                                        | not started                                                                                                                             | –                                                                                                                                                                               | 04, 14, 20     |

Also merged: first usable state ([#12](https://github.com/Zenit-Hosting-de/devbox/pull/12): `base`,
`devbox status`, readable run output, usage guide), release tag and version fix
([#21](https://github.com/Zenit-Hosting-de/devbox/pull/21)), `internal/core` extraction (part of
[#18](https://github.com/Zenit-Hosting-de/devbox/pull/18)), plugins repository scaffold
([devbox-plugins#1](https://github.com/Zenit-Hosting-de/devbox-plugins/pull/1)).

### Releases

| Version | Binaries                                   | Note                                                                                                              |
| ------- | ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| v0.0.1  | no                                         | [#2](https://github.com/Zenit-Hosting-de/devbox/pull/2)                                                           |
| v0.1.0  | no                                         | [#9](https://github.com/Zenit-Hosting-de/devbox/pull/9); tag `devbox-v0.1.0` rejected by GoReleaser, fixed by #21 |
| v0.2.0  | linux amd64/arm64 + checksums, UI embedded | [#22](https://github.com/Zenit-Hosting-de/devbox/pull/22); first release with binaries                            |
| v0.2.1  | –                                          | release PR [#25](https://github.com/Zenit-Hosting-de/devbox/pull/25) open                                         |

### Verified end to end by hand

| Area        | Result                                                                                                                                                                        |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Runtimes    | node 24 LTS, java 21, maven 3.10 installed from real sources                                                                                                                  |
| Credentials | `cred add`/`set` with npm and maven consumers against a local registry: `.npmrc` and `settings.xml` blocks, mode 0600, secret not in logs or history, `remove` restores files |
| UI          | overview, plugins, plugin detail with action form, live progress over WebSocket, history, settings, de/en, dark/light                                                         |
| Self-update | `devbox run system.update`: a 0.1.0 build updated itself to v0.2.0 and kept `devbox.previous`                                                                                 |

## Milestones mapped to the gates

| Milestone | Gate (concept)           | Work                                                                                                                                               | Exit criteria                                                                                                               | State                                                             |
| --------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| M0        | Schemas stehen           | AP-01, AP-02                                                                                                                                       | all `*.v1.json` and `openapi.json` with valid and invalid fixtures; CI, license, CLAUDE.md                                  | reached (PR #1)                                                   |
| M1        | Plugin-API eingefroren   | AP-06, AP-07, `base`, `status`, run output merged; AP-12 profiles open; `java` end to end via the protocol on top of `base`; decide plugin.v1 gaps | core with CLI, plugin host, runner, vault, config, profiles; `java` fully via protocol; `plugin.v1` frozen (additions only) | in progress; plugin API close to frozen once AP-12 is done        |
| M2        | –                        | AP-09, AP-17 (rest), AP-19, AP-23, AP-24                                                                                                           | API validated against `openapi.json`; 10 bundled plugins pass `devbox plugin test`                                          | in progress (AP-09 subset, `system`, `node` done)                 |
| M3        | –                        | AP-10, AP-11, AP-14, AP-15, AP-16, AP-18, AP-27                                                                                                    | UI on the new API; credentials with consumers; 18 bundled plugins                                                           | in progress (UI, WebSocket, credentials CLI, `npm`, `maven` done) |
| M4        | Gleichstand mit DevBox 2 | AP-20, AP-21, AP-22, AP-28, container matrix part of AP-25                                                                                         | all 20 plugins (incl. `lang-de`, `lang-en`), UI, migration, container matrix green                                          | not started                                                       |
| M5        | Version 1.0              | AP-25 (packages, package repositories), AP-26, beta, catalog                                                                                       | beta with ≥ 3 teams and own profiles, docs website, packages, ≥ 3 catalog plugins, no open high bugs                        | not started                                                       |

Plugin count towards 20: `java` (AP-13) + 5 (AP-17) + 9 (AP-18) + 3 (AP-19) + 2 (AP-28).

## Parallel work

Waves follow the "depends on" column; all packages in one wave can run in parallel sessions.

| Wave | Packages                                 | Unblocked by                 | State                                                                 |
| ---- | ---------------------------------------- | ---------------------------- | --------------------------------------------------------------------- |
| A    | AP-09, AP-12, AP-17, AP-19, AP-23, AP-24 | –                            | AP-09 subset and AP-17 partly merged; AP-12, AP-19, AP-23, AP-24 open |
| B    | AP-10, AP-11, AP-27                      | AP-09; AP-12 + AP-17 + AP-23 | AP-10 merged; AP-11 partly; AP-27 partly                              |
| C    | AP-14, AP-26                             | AP-10; AP-11                 | AP-14 merged (e2e deferred); AP-26 open                               |
| D    | AP-15                                    | AP-14                        | subset merged                                                         |
| E    | AP-16, AP-21                             | AP-15 (+ AP-11)              | AP-16 partly; AP-21 open                                              |
| F    | AP-18                                    | AP-16                        | partly (`npm`, `maven`)                                               |
| G    | AP-20, AP-22                             | AP-17–19 (+ AP-16)           | open                                                                  |
| H    | AP-25, AP-28                             | AP-20 (+ AP-14)              | AP-25 partly (GoReleaser); AP-28 open                                 |

### Next up

| Order | Package | Remaining scope                                                                         |
| ----- | ------- | --------------------------------------------------------------------------------------- |
| 1     | AP-12   | profiles; closes the `plugin.v1` freeze (M1)                                            |
| 2     | AP-23   | catalog: signature check, lock file, permission confirmation                            |
| 3     | AP-17   | `dotnet`, `python`                                                                      |
| 4     | AP-18   | `docker`, `git`, `githost`, `gradle`, `nuget`, `pip`                                    |
| 5     | AP-11   | `check`, `actions`, `group`, `setup`, `profile`, `doctor`, `schema print`, `completion` |
| 6     | AP-21   | terminal menu (TUI); needs AP-11                                                        |
| 7     | AP-24   | sandbox                                                                                 |

Orders 1 to 7 are independent of each other apart from AP-21 (needs AP-11) and can run in parallel.
Critical path (remaining): AP-12 → AP-16 (rest) → AP-18 (rest) → AP-20 → AP-25 (rest). Everything else
fits beside it.

## Decisions needed

Owner for all: maintainers.

| Decision                                                                          | Proposal                                                                                               | By when                                                                    |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| Hosting for docs and schemas at `devbox.zenit-hosting.de` (not set up)            | GitHub Pages per the concept, or the company Kubernetes with JSON manifests; decision pending          | before the plugin.v1 freeze; blocks AP-26                                  |
| Changelog duplicates (merge commits and their commits both listed)                | squash merges, or filter merge commits in the release-please changelog; decision pending               | before the next release                                                    |
| Domain for docs, schemas, package repositories                                    | `devbox.zenit-hosting.de` with `pkg.` subdomain; schema `$id` URLs already point there, not set up yet | concept: before AP-02 (overdue); at the latest before the plugin.v1 freeze |
| CGO: `make test` uses `CGO_ENABLED=1` for `-race`, release builds `CGO_ENABLED=0` | keep both, document in `CONTRIBUTING.md`                                                               | before gate M1                                                             |
| Open concept gaps (see below)                                                     | decide in one review round, plugin.v1-relevant items first                                             | before gate M1; remaining items before the AP they touch                   |
| Repository language                                                               | English for code, commits, docs; UI in German and English                                              | decided (before AP-01)                                                     |
| Network sandbox in 1.0 or 1.1                                                     | 1.1: show and log in 1.0, enforce later                                                                | before AP-24                                                               |
| Bundled plugins in the binary or only in the package                              | both: embedded for `install.sh`, files in distribution packages                                        | before AP-25                                                               |
| Static site generator for the docs                                                | one generator with JSON or JS configuration (no YAML file)                                             | before AP-26                                                               |
| First beta teams                                                                  | three teams with different registries (Nexus, Artifactory, Gitea)                                      | before gate "Gleichstand mit DevBox 2" (M4)                                |

Still open from "Nächste Schritte" in the concept: set up the domain and hosting; decide the open
decisions above. Done: both repositories exist, concept is in `docs/concept.md`, AP-01/AP-02 implemented,
example profile in `docs/examples/`, merge approvals for #9 to #24.

### Known open items

| Item                                                             | Package / area | Note                                           |
| ---------------------------------------------------------------- | -------------- | ---------------------------------------------- |
| Plugin script free text is not translated                        | AP-28          | UI and CLI texts are, plugin output is not     |
| `node` action form shows the unresolved `${node.defaultVersion}` | AP-14, AP-15   | placeholder resolution for SchemaForm defaults |
| Maven password is plain text in `settings.xml` (mode 0600)       | AP-18 `maven`  | no Maven password encryption yet               |
| CHANGELOG has duplicate entries                                  | release        | see decision on merge strategy                 |

### Way of working

Workflows: Opus for planning and review only, Sonnet for coding, Haiku for running tests.

### Open concept gaps by topic

36 numbered items were collected while planning AP-01/AP-02. AP-02 applied a proposal for most of
them; the proposals hold only after maintainer approval.

| Topic                                     | Count | Examples                                                                                                                     | Needed before  |
| ----------------------------------------- | ----- | ---------------------------------------------------------------------------------------------------------------------------- | -------------- |
| Decisions before start                    | 5     | non-JSON allow list incomplete; org path (decided: `Zenit-Hosting-de`); schema domain; `de.json` vs language packs           | M1, AP-28      |
| Schemas without a template in the concept | 8     | `catalog`, `template`, `vault` content invented; event line envelope; session ID format; cache schemas                       | M1, AP-23      |
| Examples contradicting schemas            | 8     | incomplete manifests; elided digest; open `field` keys; placeholders vs typing; `trust`, `ui.language`, `enable` in profiles | M1 (plugin.v1) |
| Scaffold, build, tests                    | 7     | maintainers / CODEOWNERS missing; CGO vs `-race`; OpenAPI generators; minimum Go version                                     | M1             |
| Formal issues in the concept text         | 5     | document title, broken code spans, embedded images, "five phases" vs four gates                                              | AP-26          |
| Later packages                            | 3     | `credentials` plugin missing from the 20; `base.packages` setting vs action; German routes and values                        | AP-14, AP-16   |

## Token estimate

Actuals so far (subagent tokens of the five workflows):

| Workflow | Subagent tokens |
| -------- | --------------- |
| 1        | ~2.2M           |
| 2        | ~2.6M           |
| 3        | ~0.6M           |
| 4        | ~0.4M           |
| 5        | ~0.3M           |
| total    | ~6.1M           |

Remaining estimate (unchanged), extrapolated from AP-06 and AP-07 (about 1–2M input and 30–60k output
tokens each).

| Size   | Packages                                               | Input per AP | Input total                                 |
| ------ | ------------------------------------------------------ | ------------ | ------------------------------------------- |
| small  | AP-12, AP-19, AP-24, AP-26, AP-28                      | 1–2M         | 5–10M                                       |
| medium | AP-10, AP-11, AP-16, AP-20, AP-21, AP-22, AP-23, AP-27 | 2–4M         | 16–32M                                      |
| large  | AP-09, AP-14, AP-15, AP-17, AP-18, AP-25               | 4–8M         | 24–48M                                      |
| total  | 19 packages                                            |              | 45–90M input (mostly cached), 1.5–3M output |

CI and review rounds add 0.5–1M input per package (about 10–19M for 19 packages).

Savings:

- one session per work package, started from this roadmap and the concept section;
- a cheaper model for mechanical work (plugin scripts, bats tests, fixtures);
- decide the concept gaps up front, so no package reopens a schema.

## devbox-plugins repository track

Repository `Zenit-Hosting-de/devbox-plugins` hosts the catalog. Version 1.0 needs at least three
plugins from outside the core.

| Step | Content                                                                                                                            | Depends on                                        | Status                                                                   |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------------ |
| 1    | Scaffold: catalog index, folder layout, CI                                                                                         | –                                                 | merged ([#1](https://github.com/Zenit-Hosting-de/devbox-plugins/pull/1)) |
| 2    | AP-23 in the core: signature check, lock file, permission confirmation; index generation and signing in the plugins repository CI  | AP-04, step 1                                     | not started                                                              |
| 3    | Out-of-core plugins: `kubectl`, `helm`, `terraform`, `go`, `rust`, `vscode`, `jetbrains-toolbox`, `podman`, `sdkman` (≥ 3 for 1.0) | plugin.v1 freeze (M1) to write; step 2 to publish | not started                                                              |
| 4    | Sync as a `plugins` repository                                                                                                     | AP-27                                             | core part done (AP-27); UI follows                                       |

Plugins in step 3 can be written in parallel with M2/M3 and tested with `devbox plugin test`
(AP-13); publishing waits for step 2.
