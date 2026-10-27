## Summary

<!-- What changes and why. -->

## Work package

<!-- For example: AP-03 Configuration. One work package per pull request. -->

AP-

## Acceptance criteria

<!-- Acceptance criteria of the work package from docs/concept.md, each backed by a test name. -->

- [ ] Criterion — test:
- [ ] Criterion — test:

## Definition of Done

- [ ] Schema and OpenAPI updated, `make generate` leaves no diff
- [ ] Tests on all affected levels, `make test` and `make lint` green
- [ ] Dry-run and idempotency tested for every new action
- [ ] Texts in `lang-de` and `lang-en`
- [ ] Documentation under `docs/` updated, examples validated
- [ ] No hard-coded address and no hard-coded path (checked by `make lint`: rule against URL literals outside `default` fields)
- [ ] Changelog-ready commit messages (Conventional Commits)
- [ ] DCO sign-off on every commit (`git commit -s`)
