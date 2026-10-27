---
name: Plugin proposal
about: Propose a new plugin for the core or the plugin catalog
title: "plugin: "
labels: plugin
assignees: ""
---

## Plugin ID

<!-- Must match ^[a-z][a-z0-9-]{1,31}$, for example `gradle`. -->

## Purpose

<!-- What does the plugin set up or maintain? -->

## Category

<!-- One of: core, system, runtime, tool, credentials, consumer, projects, remote, maintenance, language, other. -->

## Settings

<!--
Every address, path, version and switch is a setting with a default.
Default URLs must point to public sources or be empty. Use example.com in examples.
-->

| Setting | Type | Default | Purpose |
| --- | --- | --- | --- |
| | | | |

## Checks and actions

<!-- Checks read a state and change nothing. Actions change something, support dry-run and are idempotent. -->

| Kind | Name | Purpose |
| --- | --- | --- |
| check | | |
| action | | |

## Permissions needed

- Network targets:
- Readable paths:
- Writable paths:
- `sudo` (yes/no, for which step):
- Credential types:

## Sources

<!-- Where the plugin downloads from. Default URLs must be public or empty. -->

## Core or catalog

- [ ] Bundled with the core (`plugins/` in this repository)
- [ ] Catalog (`Zenit-Hosting-de/devbox-plugins`)
