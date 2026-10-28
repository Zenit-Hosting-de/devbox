# DevBox roadmap to 1.0

State as of 2026-10-28. Sources: `docs/concept.md` (sections "Fahrplan", "Arbeitspakete",
"Offene Entscheidungen", "Nächste Schritte"), the open concept gaps collected while planning
AP-01/AP-02, and the pull requests of both repositories. Work packages (AP) and their acceptance
criteria are defined in the concept; this file only tracks order, state and open decisions.

## Status per work package

| AP    | Content                                               | Status                                   | PR                                                                                                               | Depends on     |
| ----- | ----------------------------------------------------- | ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | -------------- |
| AP-01 | Scaffold, CI, release-please                          | merged                                   | [#1](https://github.com/Zenit-Hosting-de/devbox/pull/1), [#3](https://github.com/Zenit-Hosting-de/devbox/pull/3) | –              |
| AP-02 | Schemas, fixtures, `make generate`                    | merged                                   | [#1](https://github.com/Zenit-Hosting-de/devbox/pull/1)                                                          | 01             |
| AP-03 | Configuration layers, placeholders, locks             | merged                                   | [#4](https://github.com/Zenit-Hosting-de/devbox/pull/4)                                                          | 02             |
| AP-04 | Plugin host                                           | merged                                   | [#5](https://github.com/Zenit-Hosting-de/devbox/pull/5)                                                          | 02, 03         |
| AP-05 | Runner                                                | merged                                   | [#6](https://github.com/Zenit-Hosting-de/devbox/pull/6)                                                          | 04             |
| AP-06 | History                                               | PR open, CI green                        | [#10](https://github.com/Zenit-Hosting-de/devbox/pull/10)                                                        | 05             |
| AP-07 | Vault                                                 | PR open, CI green                        | [#11](https://github.com/Zenit-Hosting-de/devbox/pull/11)                                                        | 02             |
| AP-08 | `devbox sdk` commands                                 | merged                                   | [#7](https://github.com/Zenit-Hosting-de/devbox/pull/7)                                                          | 05             |
| AP-09 | REST API                                              | not started                              | –                                                                                                                | 03–07          |
| AP-10 | WebSocket                                             | not started                              | –                                                                                                                | 09             |
| AP-11 | CLI                                                   | in progress (`devbox status`)            | [#12](https://github.com/Zenit-Hosting-de/devbox/pull/12)                                                        | 09             |
| AP-12 | Profiles                                              | not started                              | –                                                                                                                | 03             |
| AP-13 | `java` reference plugin, plugin tooling               | merged                                   | [#8](https://github.com/Zenit-Hosting-de/devbox/pull/8)                                                          | 05, 08         |
| AP-14 | UI base                                               | not started                              | –                                                                                                                | 10             |
| AP-15 | SchemaForm                                            | not started                              | –                                                                                                                | 14             |
| AP-16 | Credentials                                           | in progress (CLI, `registry`)            | –                                                                                                                | 07, 13, 15     |
| AP-17 | Runtimes `system`, `base`, `node`, `dotnet`, `python` | in progress (`base`)                     | [#12](https://github.com/Zenit-Hosting-de/devbox/pull/12)                                                        | 13             |
| AP-18 | Tool plugins (9)                                      | not started                              | –                                                                                                                | 16             |
| AP-19 | `xrdp`, `cleanup`, `templates`                        | not started                              | –                                                                                                                | 13             |
| AP-20 | First-run setup and groups                            | not started                              | –                                                                                                                | 17–19          |
| AP-21 | Terminal menu                                         | not started                              | –                                                                                                                | 11, 15         |
| AP-22 | Migration from DevBox 2                               | not started                              | –                                                                                                                | 16–19          |
| AP-23 | Catalog                                               | not started (plugins repo scaffold open) | –                                                                                                                | 04             |
| AP-24 | Sandbox                                               | not started                              | –                                                                                                                | 05             |
| AP-25 | Packages, container matrix                            | not started                              | –                                                                                                                | 20             |
| AP-26 | Documentation website                                 | not started                              | –                                                                                                                | 11, 02         |
| AP-27 | Repositories                                          | not started                              | –                                                                                                                | 04, 12, 17, 23 |
| AP-28 | Language packs                                        | not started                              | –                                                                                                                | 04, 14, 20     |

Releases: v0.0.1 published ([#2](https://github.com/Zenit-Hosting-de/devbox/pull/2)); release PR
for 0.1.0 open ([#9](https://github.com/Zenit-Hosting-de/devbox/pull/9)).

The first usable state is [#12](https://github.com/Zenit-Hosting-de/devbox/pull/12), stacked on #10 and
#11: `base` (part of AP-17), `devbox status` and readable run output (part of AP-11), usage guide and this
roadmap.

## Milestones mapped to the gates

| Milestone | Gate (concept)           | Work                                                                                                                                                     | Exit criteria                                                                                                               | State           |
| --------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | --------------- |
| M0        | Schemas stehen           | AP-01, AP-02                                                                                                                                             | all `*.v1.json` and `openapi.json` with valid and invalid fixtures; CI, license, CLAUDE.md                                  | reached (PR #1) |
| M1        | Plugin-API eingefroren   | merge AP-06, AP-07; merge #12 (`base`, `status`, run output); AP-12 profiles; `java` end to end via the protocol on top of `base`; decide plugin.v1 gaps | core with CLI, plugin host, runner, vault, config, profiles; `java` fully via protocol; `plugin.v1` frozen (additions only) | in progress     |
| M2        | –                        | AP-09, AP-17 (rest), AP-19, AP-23, AP-24                                                                                                                 | API validated against `openapi.json`; 10 bundled plugins pass `devbox plugin test`                                          | not started     |
| M3        | –                        | AP-10, AP-11, AP-14, AP-15, AP-16, AP-18, AP-27                                                                                                          | UI on the new API; credentials with consumers; 18 bundled plugins                                                           | not started     |
| M4        | Gleichstand mit DevBox 2 | AP-20, AP-21, AP-22, AP-28, container matrix part of AP-25                                                                                               | all 20 plugins (incl. `lang-de`, `lang-en`), UI, migration, container matrix green                                          | not started     |
| M5        | Version 1.0              | AP-25 (packages, package repositories), AP-26, beta, catalog                                                                                             | beta with ≥ 3 teams and own profiles, docs website, packages, ≥ 3 catalog plugins, no open high bugs                        | not started     |

Plugin count towards 20: `java` (AP-13) + 5 (AP-17) + 9 (AP-18) + 3 (AP-19) + 2 (AP-28).

## Parallel work

Waves follow the "depends on" column; all packages in one wave can run in parallel sessions.

| Wave | Packages                                 | Unblocked by                                      |
| ---- | ---------------------------------------- | ------------------------------------------------- |
| A    | AP-09, AP-12, AP-17, AP-19, AP-23, AP-24 | merge of AP-06 and AP-07 (AP-09 only); others now |
| B    | AP-10, AP-11, AP-27                      | AP-09; AP-12 + AP-17 + AP-23                      |
| C    | AP-14, AP-26                             | AP-10; AP-11                                      |
| D    | AP-15                                    | AP-14                                             |
| E    | AP-16, AP-21                             | AP-15 (+ AP-11)                                   |
| F    | AP-18                                    | AP-16                                             |
| G    | AP-20, AP-22                             | AP-17–19 (+ AP-16)                                |
| H    | AP-25, AP-28                             | AP-20 (+ AP-14)                                   |

Critical path: AP-09 → AP-10 → AP-14 → AP-15 → AP-16 → AP-18 → AP-20 → AP-25. Everything else
fits beside it; start AP-09 first once AP-06 and AP-07 are merged.

## Decisions needed

Owner for all: maintainers.

| Decision                                                                          | Proposal                                                                                               | By when                                                                    |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| Merge approval for PR #10, #11, release PR #9 and devbox-plugins PR #1            | merge; CI green on all                                                                                 | now (blocks AP-09 and gate M1)                                             |
| Domain for docs, schemas, package repositories                                    | `devbox.zenit-hosting.de` with `pkg.` subdomain; schema `$id` URLs already point there, not set up yet | concept: before AP-02 (overdue); at the latest before the plugin.v1 freeze |
| CGO: `make test` uses `CGO_ENABLED=1` for `-race`, release builds `CGO_ENABLED=0` | keep both, document in `CONTRIBUTING.md`                                                               | before gate M1                                                             |
| Open concept gaps (see below)                                                     | decide in one review round, plugin.v1-relevant items first                                             | before gate M1; remaining items before the AP they touch                   |
| Repository language                                                               | English for code, commits, docs; UI in German and English                                              | decided (before AP-01)                                                     |
| Network sandbox in 1.0 or 1.1                                                     | 1.1: show and log in 1.0, enforce later                                                                | before AP-24                                                               |
| Bundled plugins in the binary or only in the package                              | both: embedded for `install.sh`, files in distribution packages                                        | before AP-25                                                               |
| Static site generator for the docs                                                | one generator with JSON or JS configuration (no YAML file)                                             | before AP-26                                                               |
| First beta teams                                                                  | three teams with different registries (Nexus, Artifactory, Gitea)                                      | before gate "Gleichstand mit DevBox 2" (M4)                                |

Still open from "Nächste Schritte" in the concept: set up the domain and GitHub Pages; decide the
open decisions above. Done: both repositories exist, concept is in `docs/concept.md`, AP-01/AP-02
implemented, example profile in `docs/examples/`.

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

Rough, extrapolated from AP-06 and AP-07 (about 1–2M input and 30–60k output tokens each).

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

| Step | Content                                                                                                                            | Depends on                                        | Status                                                                              |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- | ----------------------------------------------------------------------------------- |
| 1    | Scaffold: catalog index, folder layout, CI                                                                                         | –                                                 | PR open, CI green ([#1](https://github.com/Zenit-Hosting-de/devbox-plugins/pull/1)) |
| 2    | AP-23 in the core: signature check, lock file, permission confirmation; index generation and signing in the plugins repository CI  | AP-04, step 1                                     | not started                                                                         |
| 3    | Out-of-core plugins: `kubectl`, `helm`, `terraform`, `go`, `rust`, `vscode`, `jetbrains-toolbox`, `podman`, `sdkman` (≥ 3 for 1.0) | plugin.v1 freeze (M1) to write; step 2 to publish | not started                                                                         |
| 4    | Sync as a `plugins` repository                                                                                                     | AP-27                                             | not started                                                                         |

Plugins in step 3 can be written in parallel with M2/M3 and tested with `devbox plugin test`
(AP-13); publishing waits for step 2.
