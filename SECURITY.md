# Security Policy

## Reporting a vulnerability

Vulnerabilities are reported privately through GitHub Security Advisories, using the
private vulnerability reporting of this repository:

1. Open the **Security** tab of
   [github.com/Zenit-Hosting-de/devbox](https://github.com/Zenit-Hosting-de/devbox/security).
2. Select **Report a vulnerability**.
3. Describe the issue, the affected version (`devbox version --json`), the steps to
   reproduce it and the expected impact.

Vulnerabilities are never reported in public issues, pull requests or discussions.
Reports must not contain real tokens, passwords or other secrets.

## Response

The maintainers acknowledge a report within 5 business days and then keep the
reporter informed about the assessment, the fix and the release plan.

## Coordinated disclosure

Vulnerabilities are disclosed in coordination with the reporter. Details are published
as a GitHub Security Advisory after a fixed release is available, or earlier if the
issue is already public or actively exploited. Reporters are credited in the advisory
unless they prefer otherwise.

## Supported versions

No version of DevBox has been released yet. After the first release, security fixes
are provided for the latest minor version.

| Version | Supported |
| --- | --- |
| latest minor release | yes |
| older releases | no |

## Scope

In scope:

- the core: the `devbox` binary with API, web UI, CLI, terminal menu, plugin host,
  runner, vault and configuration
- the bundled plugins under `plugins/`
- the TypeScript SDK under `sdk/ts/` (`@zenit-hosting/devbox-sdk`)
- the install script

Plugins from the catalog repository
[github.com/Zenit-Hosting-de/devbox-plugins](https://github.com/Zenit-Hosting-de/devbox-plugins)
are reported through the private vulnerability reporting of that repository. Plugins
from third-party catalogs or repositories are reported to their respective maintainers.
