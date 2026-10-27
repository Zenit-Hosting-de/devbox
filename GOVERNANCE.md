# Governance

DevBox is an open source project published by Zenit-Hosting under the Apache License
2.0. This document describes how decisions are made, how maintainers are added, how
versions are managed and how the name is used.

## Roles

**Contributors** are everyone who opens issues, takes part in discussions, reviews
changes or submits pull requests.

**Maintainers** are listed in [MAINTAINERS.md](MAINTAINERS.md). They review and merge
pull requests, triage issues, cut releases, handle security reports and enforce the
[Code of Conduct](CODE_OF_CONDUCT.md). Areas of responsibility are recorded in
`MAINTAINERS.md` and in [.github/CODEOWNERS](.github/CODEOWNERS).

## Decision making

- Decisions are made by **lazy consensus** in the GitHub issue or pull request
  concerned: a proposal is accepted if no maintainer objects within a reasonable time.
- An objection states its reason and, where possible, an alternative.
- If consensus cannot be reached, the **majority of the maintainers** decides.
- Changes to a technical decision of the concept are recorded as an architecture
  decision record under [docs/adr/](docs/adr/).

## Maintainers

New maintainers are added after **sustained contributions** to the project (code,
reviews, documentation, plugins or support). Any maintainer can propose a contributor;
the proposal is decided by the rules above. Maintainers who are no longer active can
step down or be moved to an emeritus list in `MAINTAINERS.md`.

## Versioning

- The core (`devbox` binary, API, SDK) follows [Semantic Versioning](https://semver.org/).
- Schemas (`schema/*.v1.json`) and the plugin protocol have their own major version,
  currently `v1`, independent of the core version. Plugins and profiles therefore stay
  stable across core updates.
- Within a major version of a schema or of the protocol, only additions are allowed.
- Deprecations are announced at least one minor version in advance and produce a
  warning before the deprecated behaviour is removed.
- Releases and the changelog are created by release-please from
  [Conventional Commits](https://www.conventionalcommits.org/).

## Trademark

The name DevBox and the DevBox logo belong to Zenit-Hosting. Forks may use the code
under the terms of the Apache License 2.0, but must not present themselves as DevBox by
Zenit-Hosting. This protects users from confusion about the origin of a build.

## Communication

- Bug reports, feature requests and plugin proposals: GitHub Issues
- Questions and general discussion: GitHub Discussions
- Roadmap: GitHub Project of the repository
- Security issues: private reporting as described in [SECURITY.md](SECURITY.md)

All project communication is public, except security reports and Code of Conduct
reports.
