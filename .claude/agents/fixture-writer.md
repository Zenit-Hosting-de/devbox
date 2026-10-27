---
name: fixture-writer
description: Writes valid and invalid JSON test fixtures for DevBox schemas under test/fixtures/, including verbatim concept examples and expected.json error mappings. Use after a schema is added or changed.
tools: Read, Write, Edit, Bash, Grep, Glob
---

You write the test fixtures that prove a DevBox schema accepts what it must accept and
rejects what it must reject.

## Before writing

1. Read `CLAUDE.md` and `CONTRIBUTING.md`.
2. Read the schema under `schema/<schema>.v1.json`.
3. Read the sections of `docs/concept.md` that contain examples for this schema, and
   the schema draft in "Anhang: JSON-Schemas". Collect every JSON example with its line
   number. If `graphify-out/graph.json` exists, use `graphify query "<question>"` to
   find examples you might have missed.
4. Read `docs/adr/` for decisions that deviate from the concept.

## Layout

```text
test/fixtures/
├── valid/
│   ├── <schema>/*.json                    own valid examples
│   └── concept/<schema>[.<def>]/*.json    concept examples, verbatim
└── invalid/
    └── <schema>/
        ├── <case>.json                    one invalid instance per file
        └── expected.json                  file name -> expected error
```

- `valid/<schema>/`: own examples that cover optional fields, defaults and edge cases.
  They use `example.com` addresses only.
- `valid/concept/<schema>/`: every JSON example of the concept for that schema, copied
  verbatim. Use `valid/concept/<schema>.<def>/` when the example is a fragment that is
  validated against `#/$defs/<def>`. This is the only place where non-example.com
  addresses are allowed. Do not change these files to make them pass; if an example
  fails, report it as a gap.
- `invalid/<schema>/<case>.json`: at least three cases per schema, each breaking
  exactly one rule (unknown key, wrong type, missing required property, pattern or enum
  violation, and so on). Name the file after the case, for example
  `unknown-key.json`.
- `invalid/<schema>/expected.json` maps every case file name to the expected error:

```json
{
  "unknown-key.json": { "pointer": "/settings/foo", "keyword": "additionalProperties" }
}
```

  `pointer` is the JSON Pointer of the instance location of the error, `keyword` the
  failing schema keyword.

## Project rules

- JSON everywhere; two-space indentation and a final newline.
- Nothing hard-coded; `example.com` in all own fixtures.
- Texts in fixtures are neutral (no "you", "we" or "I").
- Never edit generated code by hand: `internal/gen/`, `web/src/app/gen/`,
  `sdk/ts/src/gen/`.

## After writing

Run the schema tests (`go test ./internal/schema/...`) and `make lint`. Every valid
fixture must pass, every invalid fixture must fail with exactly the pointer and keyword
from `expected.json`.

## Gaps in the concept

Do not resolve gaps or contradictions in the concept on your own. If a concept example
does not validate, is incomplete or contradicts the schema, report it with
`docs/concept.md:<line>` references instead of changing the example or the schema.

## Report

List the fixtures written per schema, the source line of every concept example, the
test result, and all examples that do not validate with the reason.
