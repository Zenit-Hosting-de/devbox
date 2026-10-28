# Bundled plugins

Plugins shipped with DevBox, one directory per plugin with `plugin.json`, `checks/`,
`actions/`, `i18n/` and `tests/`. They are embedded into the binary (`embed.go`) and
unpacked on first use into `${xdg:data}/devbox/builtin/<version>-<hash>/`, which comes
last in the plugin search order, so a team can replace any of them.

| Plugin | Work package |
| --- | --- |
| `java` | AP-13 (reference plugin) |
| `system` | AP-17 |

The remaining plugins follow in AP-17 to AP-19, the language packs `lang-de` and
`lang-en` in AP-28.

```bash
devbox plugin validate plugins/java
devbox plugin test --apply --set java.vendorApi=<mirror> plugins/java
```
