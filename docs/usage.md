# Using DevBox

This guide covers what the `devbox` binary can do today: build it, run the bundled
plugins, change settings, read the history and write an own plugin. DevBox is in early
development; features that are planned but not built yet are listed at the
[end](#not-yet-available).

## Build and install

There is no install script or distribution package yet, and no release has binaries yet
(the first one is the release after this change, see [Updating DevBox](#updating-devbox)).
Build from source:

```bash
git clone https://github.com/Zenit-Hosting-de/devbox.git
cd devbox
make tools                       # npm ci; needs Node.js with npm
make build                       # needs Go (version in go.mod, currently 1.26)
mkdir -p ~/.local/bin
cp bin/devbox ~/.local/bin/      # make sure ~/.local/bin is in PATH
devbox version
```

`make build` builds the npm workspaces and then `bin/devbox` with `CGO_ENABLED=0`, so
the binary has no runtime dependencies. If only the CLI is needed,
`CGO_ENABLED=0 go build -o bin/devbox ./cmd/devbox` is enough.

## Updating DevBox

Releases carry one archive per architecture and a checksum file:

```text
devbox_<version>_linux_amd64.tar.gz
devbox_<version>_linux_arm64.tar.gz
checksums.txt
```

`<version>` has no leading `v` (tag `v0.2.0` gives `devbox_0.2.0_linux_amd64.tar.gz`); the
binary `devbox` is at the top level of the archive. The `system` plugin updates DevBox
from them:

```bash
devbox status                           # system.version: installed version against the newest release
devbox run system.update --dry-run      # shows which file would be replaced
devbox run system.update
```

`system.update` asks the release list (`system.releaseUrl`, GitHub releases of the
DevBox repository by default), takes the newest release of the channel
(`system.updateChannel`: `stable`, or `prerelease` to include release candidates) that
has a binary for this machine, downloads the archive and `checksums.txt`, verifies the
SHA-256 sum and replaces the binary atomically. The previous binary stays next to it as
`devbox.previous`; to go back, `mv ~/.local/bin/devbox.previous ~/.local/bin/devbox`.

- A binary in a folder without write access (for example a package install in
  `/usr/bin`) is not touched: the action ends with exit code 3 and says so. Update such
  an installation with the package manager.
- A development build (`0.0.0-dev` or any build that is not a tagged release) is not
  replaced and not compared with releases. `-p force=true` replaces it anyway.
- A wrong checksum aborts the update and leaves the binary as it is.
- To use a mirror in the own network, point `system.releaseUrl` at a list in the same JSON
  format and with the same asset names.

Releases with binaries start with the first release after the one that introduced the
`system` plugin; earlier releases have no archives and are ignored by `system.update`.

## Where files live

DevBox follows the XDG base directory specification. Without `XDG_*` variables the paths
are:

| What                      | Path                                                                                  |
| ------------------------- | ------------------------------------------------------------------------------------- |
| User configuration        | `~/.config/devbox/config.json` (other file: `--config PATH`)                          |
| System configuration      | `/etc/devbox/config.json` (other file: `DEVBOX_SYSTEM_CONFIG`)                        |
| Own plugins               | `~/.local/share/devbox/plugins/<id>/` (default of `plugins.paths`)                    |
| Bundled plugins           | embedded in the binary, unpacked to `~/.local/share/devbox/builtin/<version>-<hash>/` |
| History                   | `~/.local/state/devbox/history/<YYYY-MM>.jsonl`, one line per finished session        |
| Session output and events | `~/.local/state/devbox/sessions/<id>.log`, `<id>.events.jsonl`                        |
| Backups                   | `~/.local/state/devbox/backups/<id>/<path>`, originals of every changed file          |
| Credentials (metadata)    | `~/.local/share/devbox/credentials.json`, no secrets                                  |
| Vault without keyring     | `~/.local/share/devbox/vault.age` and its key `vault.key`                             |

All files are JSON (or JSON Lines) with a schema in [`schema/`](../schema/). Old sessions
are removed after `history.keepDays` (default 90) or beyond `history.keepSessions`
(default 500), together with their output and backups.

## First session

The bundled plugins today are `system` (machine data, environment loader, self-update),
`base` (base packages through apt, dnf, pacman or zypper), `java` (JDKs from an
Adoptium-compatible API) and `node` (Node.js from nodejs.org/dist or a mirror, see
[Node.js](#nodejs)).

```bash
devbox plugin list                      # ID, version, state (ready, blocked, …) and reasons
devbox plugin info java                 # details; --json prints the manifest too
devbox status                           # runs every check of every ready plugin
```

`devbox status` prints one line per check and a summary:

```text
base.packages   ok       6 base packages installed
java.installed  missing  No JDK installed
java.latest     ok       Up to date

2 ok, 0 warn, 0 error, 1 missing
```

Run an action first as a dry run, then for real:

```bash
devbox run base.packages --dry-run      # lists the packages that would be installed
devbox run base.packages                # installs them; asks for the sudo password if needed
devbox run java.install --dry-run       # shows the JDK, the files and the marked blocks it would write
devbox run java.install -p version=21   # parameters with -p name=value, repeatable
```

Plugins such as `java` write environment files to `~/.config/devbox/env.d/` (for
example `JAVA_HOME` and `PATH`). Run the environment loader of the `system` plugin once
so that every new shell sources them:

```bash
devbox run system.envLoader --dry-run   # shows the blocks it would write
devbox run system.envLoader
```

It adds a marked block (`# >>> devbox:system >>>`) to `~/.bashrc` and `~/.zshrc` (setting
`system.shells`, folder `system.envDir`) and leaves the rest of those files alone. The
simple `export NAME=VALUE` lines of the environment files are also written to
`~/.config/environment.d/60-devbox.conf` (mode 0600), which graphical sessions and systemd
user services read at the next login; turn that off with `system.systemd=false`. Run the
loader again after a plugin has written a new environment file, so the systemd file picks
it up.

`devbox run` prints the steps, the planned or applied changes, notices and the result.
Actions that ask for confirmation (for example `java.remove`) prompt on the terminal;
`--yes` answers yes. Every run, dry runs included, becomes a session:

```bash
devbox history                          # newest first: ID, end, state, action, dry-run, summary
devbox history --limit 0                # all sessions
devbox logs s_01M60GM3KP7DPNSXCVR1B1BX54 # full output of one session
```

`--json` works on every reading command (`plugin list`, `plugin info`, `status`,
`history`, `logs`, `config get`, `config explain`, `version`) and prints exactly one JSON
document. `devbox run --json` prints the session events as JSON Lines instead of the
readable output.

## Node.js

The `node` plugin unpacks the official archives into `~/.local/share/node/<version>`,
verifies them against `SHASUMS256.txt`, links `current` to the default version and puts
`current/bin` in `PATH` through `~/.config/devbox/env.d/node.sh`.

```bash
devbox run node.install                       # newest release of node.channel (lts by default)
devbox run node.install -p version=20         # newest 20.x; also 22.11.0, lts, current
devbox run node.use -p version=22             # switch the default (a major or a full version)
devbox run node.update                        # newest patch of every installed major version
devbox run node.globals                       # install node.globalPackages into the default
devbox run node.remove -p version=20.18.0     # asks for confirmation
```

- `node.distUrl` points to a mirror (it needs `index.json`, `SHASUMS256.txt` and the
  archives in the layout of nodejs.org/dist); `node.defaultVersion` is a major version
  that `node.install` uses when no version is given.
- `node.globalPackages` (for example `["typescript@5"]`) is installed with `npm install -g`
  into every version that is installed or updated. On `node.update`, packages installed
  globally in the old patch version are taken along to the new one.
- `node.keepPatches` keeps that many patch versions per major version; the default is
  never removed. `node.manager` knows `direct` only; support for nvm is planned.

## Settings

Every value a plugin needs is a setting `<plugin>.<name>` with a default in its
`plugin.json`. Change it, check where the value comes from, and reset it:

```bash
devbox config get java.vendorApi
devbox config set java.vendorApi https://mirror.example.com/adoptium/v3
devbox config explain java.vendorApi
devbox config unset java.vendorApi
```

`config explain` shows the effective value and every layer below; `*` marks the winner:

```text
java.vendorApi = "https://mirror.example.com/adoptium/v3"
    plugin: "https://api.adoptium.net/v3"
  * user: "https://mirror.example.com/adoptium/v3"
```

Layers, the later one wins: plugin default, system file, profiles (in the order of
`profiles` in the configuration), user file, environment and call:

```bash
DEVBOX_SET__JAVA__DEFAULTVERSION=17 devbox run java.install   # environment, this run only
devbox --set java.defaultVersion=17 run java.install          # call, this run only
```

A profile can lock a key; `config set` then refuses it. Other parts of `config.json`
(`plugins`, `profiles`, `network`, `history`, …) are edited with `devbox config edit`
(opens `$VISUAL` or `$EDITOR`) and checked with `devbox config validate`. The proxy and CA
file from `network` are passed to every plugin.

## Credentials

A credential instance has a type (defined by a plugin, e.g. `registry`), fields, a secret
and consumers: plugins that write it into the configuration of their tool. Profiles can
prepare instances (fields preset or locked, consumers listed), so only the secret is
missing. The metadata lives in `~/.local/share/devbox/credentials.json` (mode 0600, no
secrets); the secret goes into the vault (`vault.backend`: the Secret Service, or the
age-encrypted `vault.age` with the key `vault.key`, both 0600).

```bash
devbox cred add company --type registry \
  --field url=https://nexus.example.com --field layout=nexus --field username=m.muster \
  --consumer 'npm:repository=npm-group,scopes=["@example"]' --consumer maven:mirror=maven-public
devbox cred set company                      # hidden input; or: … | devbox cred set company --from-stdin
devbox cred set company --expires 2027-01-25 # with an expiry date
devbox cred list                             # --json for the merged view of profile and user
devbox cred verify company                   # runs the verify action of the type
devbox cred apply company --dry-run          # runs every consumer's apply
devbox cred remove company                   # runs every consumer's remove, then deletes the secret
```

`cred set` stores the secret, runs the verification of the type (for `registry` a request
to the registry; the paths per layout are the setting `registry.verifyPaths`) and, when it
succeeds, applies every consumer. Array options of `--consumer` (like `scopes`) take JSON
(`scopes=["@a","@b"]`) or plain values, one (`scopes=@a`) or several separated by `;`
(`scopes=@a;@b`); the same goes for array settings in `--set`. Consumers run one after another, each as a session in
`devbox history`; one failing consumer does not stop the others, and the exit code is 1 if
any failed. Consumers of disabled plugins are skipped. `cred remove` keeps the secret when a
consumer fails to remove it, unless `--force` is given; for an instance a profile prepares,
only the secret and the consumers the user added go. The check `credentials.expiry` warns
`credentials.warnDays` (default 14) days before a secret expires.

Plugins get the credential only through the request file, never through the environment
or the command line; their output is masked. A consumer reads it with `devbox sdk instance`,
`devbox sdk field NAME`, `devbox sdk option NAME` and `devbox sdk secret`.

## Dry run and backups

- `--dry-run` sets `DEVBOX_DRY_RUN=1` for the plugin. It still reads, checks and
  downloads metadata, but changes nothing and reports every change it would make.
- Plugins edit existing files through `devbox sdk block` and `devbox sdk json-set`.
  Before the first change of a file in a session the original is copied to
  `~/.local/state/devbox/backups/<session>/<path>`; `devbox logs SESSION --json` lists the
  backups of a session.
- Changes to shell files are marked blocks (`# >>> devbox:<plugin> >>>` …
  `# <<< devbox:<plugin> <<<`), replaced in place on the next run instead of appended.
- Actions are idempotent: a second run with the same values changes nothing.

## Exit codes

| Code | Meaning                                                                    |
| ---- | -------------------------------------------------------------------------- |
| 0    | success, also when there was nothing to do; `devbox status` always exits 0 |
| 1    | error                                                                      |
| 2    | invalid parameter, setting, configuration, or unknown plugin or action     |
| 3    | a requirement is missing (plugin `blocked`, or the action reported it)     |
| 4    | confirmation refused (or no terminal to ask and no `--yes`)                |
| 130  | cancelled with Ctrl+C                                                      |

Any other exit code from 1 to 127 of a plugin action is passed through unchanged.

## Writing an own plugin

```bash
devbox plugin new hello                             # copies the bash template to ./hello
devbox plugin validate hello                        # manifest, scripts (shellcheck) and permissions
devbox plugin test hello                            # checks and dry runs in a sandbox; --apply also runs actions twice
cp -r hello ~/.local/share/devbox/plugins/          # the default plugin path
devbox run hello.apply --dry-run
```

To load plugins from other directories, set `plugins.paths` in `config.json`; the list
replaces the default. A plugin with the same ID as a bundled one replaces it, because
bundled plugins are searched last. The protocol, the environment and the `devbox sdk`
helpers are described in [concept.md](concept.md#plugin-protokoll) (German) and in
[templates/plugin/](../templates/plugin/).

## Not yet available

These are planned (see [roadmap.md](roadmap.md)) but do not exist in the binary yet:

- install script, distribution packages
- web UI, `devbox serve`, `devbox service …`, terminal menu (`devbox` without arguments)
- `devbox setup`, `devbox group run` (for example `update-all`), `devbox check`,
  `devbox actions`, `devbox status --strict` (exit codes 5 and 6)
- the registry consumers `npm`, `maven`, `nuget`, `pip`, `docker` and the credential types
  `git-host`, `ssh-key`, `generic`; the event `credential.changed`
- `devbox profile …`; profiles from HTTPS or Git sources (only `file://` works today)
- `devbox plugin install|update|remove` and plugin catalogs
- `devbox doctor`, `devbox schema print`, `devbox migrate`, `devbox completion`
- global flags `--quiet`, `--no-color`, `--lang`, `--socket`
- the remaining bundled plugins (`node`, `dotnet`, `python`, `docker`, `git`, `xrdp`,
  `cleanup`, …) and plugin templates for TypeScript, Python and Go
