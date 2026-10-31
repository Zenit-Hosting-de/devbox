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
| Plugin repositories       | `~/.local/share/devbox/repositories/<name>/<revision>/` and `state.json`              |
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
Adoptium-compatible API), `node` (Node.js from nodejs.org/dist or a mirror, see
[Node.js](#nodejs)), `maven` (Maven and `settings.xml`), `npm` (`.npmrc`), `registry`
(the credential type for package registries) and `credentials` (expiry warnings).

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
`history`, `logs`, `config get`, `config explain`, `repo list`, `version`) and prints exactly one JSON
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
| 3    | a requirement is missing (plugin `blocked`, the action reported it, or `git` or the network is missing for a repository) |
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
replaces the default. Plugins are searched in this order: `plugins.paths`, the active
revisions of the enabled plugin repositories (by name, see
[Plugin repositories](#plugin-repositories)), then the bundled ones. The first plugin
with an ID wins, so a plugin with the same ID as a bundled one replaces it. The protocol, the environment and the `devbox sdk`
helpers are described in [concept.md](concept.md#plugin-protokoll) (German) and in
[templates/plugin/](../templates/plugin/).

## Plugin repositories

A plugin repository is a Git repository, a tar.gz archive or a local directory that holds
one or more plugin folders (each with a `plugin.json`). Its plugins appear like the bundled
ones, with the repository as their origin (`devbox plugin list` shows `builtin`, `path` or
`repository`). Only repositories of kind `plugins` work so far; entries of the other kinds
(`catalog`, `profile`, `templates`, `packages`) are listed with the state `unsupported`
and otherwise ignored.

Repositories are entries in `repositories` of `config.json` (user file), of a profile or
of the system file. Add one with the CLI, or edit the file:

```bash
devbox config edit
```

```text
{
  "repositories": {
    "team-plugins": {
      "kind": "plugins",
      "source": "git+https://git.example.com/platform/devbox-plugins.git",
      "ref": "main",
      "path": "plugins",
      "trust": "prompt"
    }
  }
}
```

| Field     | Meaning                                                                                                                  |
| --------- | ------------------------------------------------------------------------------------------------------------------------ |
| `kind`    | `plugins`                                                                                                                |
| `source`  | `git+https://`, `git+ssh://`, `git+file://` (local Git repository), `https://` (tar.gz, at most 100 MiB) or `file://` (local directory, `file:///absolute/path`) |
| `ref`     | Branch, tag or commit; Git sources only. Default: the HEAD of the remote                                                 |
| `path`    | Sub-directory that holds the plugin folders                                                                              |
| `trust`   | `prompt` (default) or `pinned`, see below                                                                                |
| `enabled` | `false` keeps the entry but ignores it                                                                                   |
| `locked`  | In a profile or the system file: the user cannot replace the entry                                                       |

The name of an entry (`^[a-z][a-z0-9-]{1,31}$`) is its ID in the CLI and the API. Entries
with the same name in several layers: the later layer wins (system file, profiles, user
file), unless the earlier entry is `locked`.

### Commands

```bash
devbox repo list                        # name, state, kind, origin, source, revisions
devbox repo add team-plugins --source git+https://git.example.com/platform/devbox-plugins.git \
  --ref main --path plugins             # writes the entry to the user file and syncs it
devbox repo set team-plugins --path plugins   # changes the entry (user file only) and syncs it
devbox repo sync                        # every enabled repository; or: devbox repo sync team-plugins
devbox repo approve team-plugins        # make the pending revision active
devbox repo discard team-plugins        # drop the pending revision
devbox repo remove team-plugins         # remove the entry and its files
```

`repo add` takes `--source`, `--ref`, `--path` and `--trust prompt|pinned`. Plugins are
the only kind so far, so there is no `--kind`.

`repo set NAME` changes an existing entry of the user file with the same flags, all
optional; only the flags that are given change something, and an empty value removes
`--ref` or `--path` (`--path ''`). The result is validated like a new entry, then the
repository is synced: a changed `ref` or `path` yields a pending revision that needs
approval (trust `prompt`). Entries of the system file and of profiles are refused
(`conflict`).

The plugin folders must be direct sub-folders of `path`. When the folder holds no plugin
(a typical mistake is an empty `path` while the plugins live in `plugins/`), the sync
searches the revision (three levels deep, hidden folders skipped) for folders that do and
says so:

```text
team-plugins: revision 3f9a1c0b2d44 waits for approval
  no plugins found; plugins exist below: plugins (set with --path)
approve with: devbox repo approve team-plugins
```

`devbox repo set team-plugins --path plugins` fixes it. The folders are recorded as
`suggestedPaths` on the revision in `state.json` and in the API.

`repo sync` fetches the source and lists the plugins of the new revision as added (`+`),
changed (`~`), removed (`-`) or unchanged (`=`), together with their declared permissions
and the commands they require. Permissions that the active revision did not have are
marked `(new)`:

```text
team-plugins: revision 3f9a1c0b2d44 waits for approval (active: 81c2e07a5f19)
  ~ hello 1.1.0 (was 1.0.0) [changed]
      network      https://api.example.com  (new)
      files.write  ~/.config/hello
      commands     curl, jq
  + extra 0.1.0
      network      https://downloads.example.com  (new)
  = other 1.0.0 [unchanged]
approve with: devbox repo approve team-plugins
```

### Trust: prompt and pinned

- `prompt` (default): a synced revision is stored but not used. It is pending until it is
  approved. `devbox repo approve NAME` shows the report above and asks on the terminal.
  Without a terminal it needs `--yes`, otherwise it exits with code 2. The first revision
  needs approval too. Until then the previous active revision (if any) stays in use.
- `pinned`: only for Git sources whose `ref` is a full 40-character commit. The synced
  revision becomes active without approval, because the commit is the approval.

A `trust` object (Sigstore signature) and `credential` (access to private repositories)
are not supported yet and are refused with an error; both follow later.

### Where the files live

Each revision is stored under `~/.local/share/devbox/repositories/<name>/<revision>/`
(the first 12 characters of the revision). `state.json` next to them
(`schema/repository-state.v1.json`) records the active and the pending revision, the last
sync and the last error. Syncing again with the same revision changes nothing. Plugins
are only read from the active revision; a pending one is not executed.

### Web UI and API

The REST API has `GET /repositories`, `POST /repositories`, `PATCH /repositories/{name}`
(any of `source`, `ref`, `path`, `trust`; `null` removes `ref` and `path`; answers with the
synced repository, `409` for an entry of the system file or a profile),
`POST /repositories/{name}/sync`, `POST /repositories/{name}/approve`,
`DELETE /repositories/{name}/pending` and `DELETE /repositories/{name}`. A missing `git` or
network answers `424` with the problem code `prerequisite`. After a sync, an approval or a switch, the WebSocket topic `plugins` sends
a `plugins` message with the changed plugin IDs and repositories.

In the web UI the "Sources" tab of the Plugins page lists the repositories. "Add source"
and "Edit" (sources of the user file) open the same form; the name cannot be changed. When
the newest revision of a source holds no plugins, a warning offers one button "Use path
<folder>" per suggested folder, which sets the path and syncs.

### Switching plugins on and off

```bash
devbox plugin disable hello             # writes the ID to plugins.disabled in the user file
devbox plugin enable hello
```

Plugins of the `core` category cannot be switched off. A plugin that the system file or a
profile switched off cannot be switched on by the user. The API has `PATCH /plugins/{id}`
with the body `{"enabled": true}` for the same.

## Web service (`devbox serve`)

`devbox serve` starts the REST API (`/api/v1`, described in `api/openapi.json`) and
serves the web interface from the same binary. It logs JSON lines to stderr: one line
`listening` with the URL (port `0` prints the real port), one line per request (method,
path, status, duration; never bodies or query strings).

```sh
devbox serve                    # http://127.0.0.1:7878 (server.listen)
devbox serve --listen 127.0.0.1:0 --open
```

| Flag                 | Meaning                                                                                                                                                                                           |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--listen ADDR`      | Address to listen on. Default: `server.listen` (user file, then system file, then the schema default `127.0.0.1:7878`).                                                                           |
| `--open`             | Open the URL with `$BROWSER`, else `xdg-open`. A failure is only a warning.                                                                                                                       |
| `--socket PATH`      | Unix socket. Default `$XDG_RUNTIME_DIR/devbox/api.sock` (directory 0700, socket 0600, a stale socket is replaced). A socket directory that does not exist is created with 0700; an existing one, for example `$HOME` or `/tmp`, keeps its mode. Without `XDG_RUNTIME_DIR` and `--socket` the socket is skipped with a warning. |
| `--allow-origin URL` | Allow one more origin for this run, on top of `server.allowedOrigins`. Repeatable.                                                                                                                |

**Listen address and login.** `server.auth` is `local` (default) or `off`; both are
accepted on loopback addresses only (`127.0.0.0/8`, `::1`, `localhost`). `off` logs a
warning and behaves like `local` in this milestone. A non-loopback address, or
`server.auth` set to `pam`, makes `devbox serve` exit with code 2: both need the PAM
login and TLS, which are not available yet.

**Protection of the TCP port.** Every request needs a `Host` header that names the listen
address (or `localhost`, `127.0.0.1`, `[::1]` with the same port), which stops DNS
rebinding. Modifying requests (`POST`, `PUT`, `PATCH`, `DELETE`) also need the header
`X-DevBox-Request: 1` and an `Origin` that is the origin of the server itself,
`server.allowedOrigins` or `--allow-origin`; otherwise the answer is `403` with the
problem code `csrf`. Requests over the Unix socket skip these checks, because the file
mode (0600 in a 0700 directory) is the protection. Responses carry a strict
`Content-Security-Policy`, `X-Content-Type-Options: nosniff` and `Referrer-Policy:
no-referrer`.

**Development.** The Angular dev server on `http://localhost:4200` calls the API when the
service is started with `devbox serve --allow-origin http://localhost:4200` (`make dev`
does this once the web UI is in). Without a web build (`make build`) the binary serves a
short placeholder page for the UI; the API works either way.

**Behaviour.**

- The configuration and the plugins are read again for every request, so the files stay
  the single source of truth. `PATCH /settings` writes the user file like
  `devbox config set`; actions started over the API run, are recorded and pruned exactly
  like `devbox run` (origin `ui` for a browser, `api` otherwise).
- Actions of one plugin queue behind each other, also across requests. Starting an
  action that is already queued or running answers `409 conflict`; an action with a
  `confirm` prompt needs `"yes": true` unless it is a dry run.
- Check results are cached in memory only (nothing is written to disk in this
  milestone). All checks run when the service starts, results older than the `cache`
  time of the check are refreshed when read, and the checks of a plugin run again after
  one of its actions has finished.
- On SIGINT or SIGTERM the service stops accepting requests, stops live sessions (they
  end as `stopped` and are recorded in the history) and waits for them.

**Restart after an update.** An action can end with the notice `restart-service`
(`sdk emit notice restart-service "…"`); `system.update` does so after it replaced the
binary. When such a session ends successfully, `devbox serve` announces the restart with a
`restart` message on the WebSocket, waits until all other sessions have ended (it never
stops a running action; a session started in the last moment is refused with
`503`), closes the WebSocket connections with status 1012 and then replaces itself by the
binary at the path it was started from (`exec`: same arguments and environment, same
process ID, and `DEVBOX_RESTARTED=1` so `--open` does not open a second browser window).
The web UI shows "DevBox is restarting with the new version", reconnects and reloads the
page when the version changed. If the binary cannot be started, the service logs the error
and keeps serving with the old version; stop and start it by hand then. A supervisor that
manages the service sees no restart, because the process ID stays.

**Not available yet:** the PAM login (`/auth/*`), the login rate limit, TLS and
non-loopback addresses; the command line client over the Unix socket; setting a
credential secret over the API (`PUT /credentials/{name}/secret`); and the endpoints for
groups, setup, profiles and plugin catalogs.

### WebSocket

`GET /api/v1/ws` upgrades to a WebSocket that carries everything the web UI shows live.
One connection serves one browser window. Messages are JSON objects with a `type`;
terminal bytes are Base64. The messages are described by `WsClientMessage` and
`WsServerMessage` in `api/openapi.json`. Over TCP the upgrade needs an allowed `Origin`
(the same rule as for modifying requests); otherwise the answer is `403` with the problem
code `csrf`. Over the Unix socket no `Origin` is needed.

| Direction | Message                                  | Meaning                                                                                                                                    |
| --------- | ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| to core   | `subscribe` `topics`, optional `fromSeq` | Topics: `checks`, `sessions`, `settings`, `plugins`, `session:<id>`. `fromSeq` applies to the session topics of the message.                          |
| to core   | `unsubscribe` `topics`                   | Leave topics.                                                                                                                              |
| to core   | `input` `session`, `data`                | Keyboard input for the PTY of a running session (at most 64 KiB decoded), for example a `sudo` password. It is never logged.               |
| to core   | `resize` `session`, `cols`, `rows`       | New terminal size. Before the process has a terminal the size is kept and applied at the start.                                            |
| to client | `output`, `event`                        | Terminal output and protocol messages of a session. Both carry `seq`; output and events of a session share one counter.                    |
| to client | `session`, `check`, `settings`           | A session was created or changed (progress at most 4 times per second), a check finished, settings were changed through the API.           |
| to client | `plugins`                            | Plugins or plugin repositories changed: `ids` (plugin IDs), `repositories` (repository names). |
| to client | `restart`                                | The core restarts with a new binary (see "Restart after an update"). The connection closes shortly after with status 1012; clients that connect while the restart is pending get the message too. |
| to client | `gap`, `error`                           | The requested `fromSeq` is older than the buffer; a client message was refused (`validation`, `not-found`, `conflict`), the connection stays open. |
| both      | `ping`, `pong`                           | The core sends a `ping` every 30 seconds and answers a `ping` with a `pong`. A connection that sent nothing for 75 seconds is closed.      |

**Resuming.** Subscribing to `session:<id>` first sends the current `session` summary and
then every buffered `output` and `event` message with `seq` of at least `fromSeq` (all
buffered messages without `fromSeq`), in order, followed by the live ones without gap or
duplicate. A window that lost its connection keeps the last `seq` it saw and subscribes
again with `fromSeq` one higher. If `fromSeq` is older than the buffer, a `gap` message
(`fromSeq`, `firstSeq`) comes first and streaming continues at `firstSeq`; the missing
part is in `GET /sessions/{id}/log`.

**Buffer.** The core keeps the last 2 MiB of payload per session in memory. The buffers
of finished sessions stay for 10 minutes, at most 32 of them. A session that is neither
running nor buffered is answered with an `error` of code `not-found` and the `topic`;
its output is then only available over REST.

**Slow clients.** Messages are queued per connection. A client that falls more than 1024
messages behind is closed with status 1008 (`slow consumer`); it reconnects and resumes
with `fromSeq`. A server shutdown closes connections with status 1001.

Not available yet: the `credentials` topic, which waits for the credential operations of
the API.

## Not yet available

These are planned (see [roadmap.md](roadmap.md)) but do not exist in the binary yet:

- install script, distribution packages
- web UI (the placeholder page only), `devbox service …`, terminal menu (`devbox` without arguments)
- `devbox setup`, `devbox group run` (for example `update-all`), `devbox check`,
  `devbox actions`, `devbox status --strict` (exit codes 5 and 6)
- the registry consumers `npm`, `maven`, `nuget`, `pip`, `docker` and the credential types
  `git-host`, `ssh-key`, `generic`; the event `credential.changed`
- `devbox profile …`; profiles from HTTPS or Git sources (only `file://` works today)
- `devbox plugin install|update|remove` and plugin catalogs
- plugin repositories: the web UI (Plugins page with "Installed" and "Sources"), the kinds
  `catalog`, `profile`, `templates` and `packages`, credentials and Sigstore signatures
  (`credential`, `trust` object), `devbox repo init`
- `devbox doctor`, `devbox schema print`, `devbox migrate`, `devbox completion`
- global flags `--quiet`, `--no-color`, `--lang`, `--socket`
- the remaining bundled plugins (`node`, `dotnet`, `python`, `docker`, `git`, `xrdp`,
  `cleanup`, …) and plugin templates for TypeScript, Python and Go
