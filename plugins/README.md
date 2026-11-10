# Bundled plugins

Plugins shipped with DevBox, one directory per plugin with `plugin.json`, `checks/`,
`actions/`, `i18n/` and `tests/`. They are embedded into the binary (`embed.go`) and
unpacked on first use into `${xdg:data}/devbox/builtin/<version>-<hash>/`, which comes
last in the plugin search order, so a team can replace any of them.

| Plugin        | Work package                                                                                                                                                    |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `java`        | AP-13 (reference plugin)                                                                                                                                        |
| `credentials` | AP-16 (credential instances and their consumers)                                                                                                                |
| `githost`     | AP-16 (credential type `git-host`, verification against the API of the host; also the access of private plugin repositories, AP-27)                             |
| `registry`    | AP-16 (credential type `registry`, verification per layout)                                                                                                     |
| `system`      | AP-17 (host data, environment loader, self-update)                                                                                                              |
| `base`        | AP-17 (base packages, system update)                                                                                                                            |
| `node`        | AP-17                                                                                                                                                           |
| `dotnet`      | AP-17 (SDKs by channel or version from `metadataUrl`, `installScriptUrl`, workloads)                                                                            |
| `python`      | AP-17 (uv from `uvInstallUrl`, Python versions through uv, Python downloads redirected by `pythonMirror`)                                                       |
| `npm`         | AP-18 (registry consumer for `.npmrc` or the environment)                                                                                                       |
| `maven`       | AP-18 (install, update, registry consumer for `settings.xml`; mode `file` only, Maven reads no passwords from the environment)                                  |
| `git`         | AP-18 (identity, defaults, consumer for `git-host`: token through a credential helper, default `store` in a DevBox file with mode 0600)                         |
| `gradle`      | AP-18 (registry consumer: `gradle.properties` or `ORG_GRADLE_PROJECT_*` in `env.d`)                                                                             |
| `nuget`       | AP-18 (registry consumer: package sources in `NuGet.Config`, credentials in the file or `NuGetPackageSourceCredentials_*` in `env.d`)                           |
| `pip`         | AP-18 (registry consumer for pip and uv: `pip.conf`, `uv.toml` and `.netrc`, or `PIP_INDEX_URL` and `UV_DEFAULT_INDEX`)                                         |
| `docker`      | AP-18 (Docker Engine from the packages of the distribution or of Docker, group, `daemon.json`, registry consumer: logins in `~/.docker/config.json`, mode 0600) |
| `cleanup`     | AP-19 (caches, old build folders in projects and Docker; sizes in the dry run; contributes to `cleanup-all`)                                                    |
| `templates`   | AP-19 (projects from templates in Git sources: `sync`, check `available`, `new`, `hooks` for the commands of a template)                                        |
| `xrdp`        | AP-19 (xrdp with XFCE: install, `xrdp.ini` and `sesman.ini`, session start, polkit rule, sound module, enable, restart, disable)                                |

The language packs `lang-de` and `lang-en` follow in AP-28.

```bash
devbox plugin validate plugins/java
devbox plugin test --apply --set java.vendorApi=<mirror> plugins/java
```
