# Plugin templates

Templates for `devbox plugin new NAME --lang LANG`, one directory per language. Files
may contain `{{id}}` and `{{name}}`, which are replaced when a plugin is created.

| Template | Content |
| --- | --- |
| `bash/` | manifest with two settings, a check, an idempotent action using `devbox sdk block`, bats tests |

Templates for TypeScript, Python and Go follow with the SDKs.
