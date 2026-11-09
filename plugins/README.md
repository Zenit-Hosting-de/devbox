# Bundled plugins

Plugins shipped with DevBox, one directory per plugin with `plugin.json`, `checks/`,
`actions/`, `i18n/` and `tests/`. They are embedded into the binary (`embed.go`) and
unpacked on first use into `${xdg:data}/devbox/builtin/<version>-<hash>/`, which comes
last in the plugin search order, so a team can replace any of them.

| Plugin   | Work package                                                                                                                   |
| -------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `java`   | AP-13 (reference plugin)                                                                                                       |
| `githost` | AP-16 (credential type `git-host`, verification against the API of the host; also the access of private plugin repositories, AP-27) |
| `maven`  | AP-18 (install, update, registry consumer for `settings.xml`; mode `file` only, Maven reads no passwords from the environment) |
| `gradle` | AP-18 (registry consumer: `gradle.properties` or `ORG_GRADLE_PROJECT_*` in `env.d`) |
| `nuget`  | AP-18 (registry consumer: package sources in `NuGet.Config`, credentials in the file or `NuGetPackageSourceCredentials_*` in `env.d`) |
| Plugin   | Work package                                                                                                                   |
| -------- | ------------------------                                                                                                       |
| `java`   | AP-13 (reference plugin)                                                                                                       |
| `system` | AP-17                                                                                                                          |
| `node`   | AP-17                                                                                                                          |

The remaining plugins follow in AP-17 to AP-19, the language packs `lang-de` and
`lang-en` in AP-28.

```bash
devbox plugin validate plugins/java
devbox plugin test --apply --set java.vendorApi=<mirror> plugins/java
```
