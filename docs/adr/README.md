# Architecture decision records

An architecture decision record (ADR) documents a technical decision and its reasons.
A technical decision from [docs/concept.md](../concept.md) is changed only through an
accepted ADR, written before the change.

## Format

- One Markdown file per decision, named `NNNN-title.md` with a four-digit sequential
  number and a short lowercase title in kebab case, for example
  `0001-repository-location.md`.
- Header lines: title (`# NNNN. Title`), `Status` (`proposed`, `accepted`,
  `deprecated` or `superseded by NNNN`) and `Date` (`YYYY-MM-DD`).
- Sections:
  - `## Context`: the situation and the forces that require a decision
  - `## Decision`: the decision, stated plainly
  - `## Consequences`: what follows from the decision, including drawbacks
- Accepted ADRs are not rewritten. A changed decision gets a new ADR that supersedes
  the old one, and the old one is marked `superseded by NNNN`.

## Records

| Number | Title | Status |
| --- | --- | --- |
| [0001](0001-repository-location.md) | Repository location | accepted |
