# 0001. Repository location

- Status: accepted
- Date: 2026-10-27

## Context

The concept ([docs/concept.md](../concept.md), sections "Kurzfassung", "Name und
Auftritt" and "Repository, Build und Releases") names `github.com/zenit-hosting/devbox`
as the core repository, `github.com/zenit-hosting/devbox-plugins` as the plugin
catalog repository and `github.com/zenit-hosting/devbox` as the Go module path.

The GitHub organisation that actually hosts the project is `Zenit-Hosting-de`. A Go
module path must match the location from which the module is fetched, and links in
documentation, issue templates and security instructions must resolve.

The concept also lists the domain for documentation, schemas and package repositories
as an open decision, with the proposal `devbox.zenit-hosting.de` and a `pkg.`
subdomain, to be decided before AP-02 because the schema `$id` URLs depend on it.

## Decision

- The core repository is `github.com/Zenit-Hosting-de/devbox`.
- The Go module path is `github.com/Zenit-Hosting-de/devbox`.
- The plugin catalog repository is `github.com/Zenit-Hosting-de/devbox-plugins`.
- All links in code, documentation and repository files use these locations.
- Schema `$id` URLs keep the proposed domain `devbox.zenit-hosting.de`
  (`https://devbox.zenit-hosting.de/schema/<name>.v1.json`); the open decision in the
  concept is adopted with the proposal for AP-02.
- Published package names keep the `zenit-hosting` scope or prefix
  (`@zenit-hosting/devbox-sdk`), as the concept requires.
- `docs/concept.md` stays unchanged as the reference document. Where it names
  `github.com/zenit-hosting/...`, this ADR takes precedence.

## Consequences

- Imports use `github.com/Zenit-Hosting-de/devbox/...`. Moving the repository to
  another organisation later changes the module path and is a breaking change for
  external Go importers.
- Readers of the concept find a different organisation name there; this ADR explains
  the difference.
- Concept examples copied verbatim into `test/fixtures/valid/concept/` keep the
  original `github.com/zenit-hosting/...` addresses.
- Schema identifiers do not depend on the GitHub organisation. The domain
  `devbox.zenit-hosting.de` must be registered and serve the schemas before the first
  release, so that editors can resolve `$schema` references.
