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

## .NET

The `dotnet` plugin installs .NET SDKs with the official `dotnet-install.sh` into
`~/.local/share/dotnet` and sets `DOTNET_ROOT` and `PATH` through
`~/.config/devbox/env.d/dotnet.sh`. Versions come from the release metadata.

```bash
devbox run dotnet.install                     # newest SDK of dotnet.channel (lts by default)
devbox run dotnet.install -p version=8.0      # a channel (8.0, lts, sts) or a full SDK version
devbox run dotnet.update                      # newest SDK of every installed channel
devbox run dotnet.workloads                   # install dotnet.workloads (for example ["wasm-tools"])
devbox run dotnet.remove -p version=8.0.404   # asks for confirmation
```

- `dotnet.metadataUrl`, `dotnet.installScriptUrl` and `dotnet.feedUrl` point to mirrors;
  `dotnet.keepPatches` keeps that many SDKs per channel.

## Python

The `python` plugin installs [uv](https://docs.astral.sh/uv/) from `python.uvInstallUrl`,
then the Python versions of `python.versions` through uv, and command line tools of
`python.tools` with `uv tool install`. `PATH` is set through `~/.config/devbox/env.d/python.sh`.

```bash
devbox run python.install                     # uv, python.versions and python.tools
devbox run python.use -p version=3.12         # default version (python3 on PATH)
devbox run python.update                      # uv self update, newest patches, tool upgrades
devbox run python.tools                       # install python.tools (for example ["ruff"])
devbox run python.remove -p version=3.11      # asks for confirmation
```

- `python.pythonMirror` redirects the Python downloads of uv (`UV_PYTHON_INSTALL_MIRROR`)
  to a mirror; `python.selfUpdate` turns off `uv self update` where uv comes from a package.

## Docker

The `docker` plugin installs Docker Engine, either from the packages of the distribution
(`docker.source` = `distro`, the default) or from the package repository of Docker
(`docker`, apt and dnf), and manages `daemon.json`.

```bash
devbox run docker.install                     # packages, then enables and starts the service (sudo)
devbox run docker.group                       # adds the user to the docker group (new login needed)
devbox run docker.configure                   # registryMirrors, logDriver, logOptions, defaultAddressPools
devbox run docker.update                      # upgrades the installed packages
```

- `docker.configure` changes only the keys it manages in `/etc/docker/daemon.json` and keeps
  all others; emptying a setting puts the previous value back. `docker.serviceReload`
  (`reload`, `restart`, `none`) says what happens to the service afterwards.
- `docker.repoUrl`, `docker.repoKeyUrl` and the package lists are settings, so a mirror can
  replace the repository of Docker.
- As a consumer of `registry` (option `host`), the plugin writes the login for the host
  into `~/.docker/config.json` (mode 0600). A login for that host that DevBox did not
  write is not overwritten.

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

## Profiles

A profile holds the defaults of a team: settings (possibly locked), plugins to switch on or
off, prepared credential instances, repositories. It is one JSON file (`profile.v1.json`)
from one of these sources:

| Source | Example | Read from |
| --- | --- | --- |
| local file | `file://${xdg:config}/devbox/team.json` | the file itself, at every start |
| web server | `https://example.com/devbox/profile.json` (at most 1 MiB) | the cached copy |
| Git repository | `git+https://git.example.com/platform/devbox-profile.git` or `git+ssh://…`, with `ref` and `path` (default `profile.json`) | the cached copy |

Remote profiles are cached under `~/.cache/devbox/profiles/<key>/profile.json`. A cached copy
is used as long as it exists; `devbox profile update` fetches again. A failed fetch keeps the
previous copy.

```bash
devbox profile add git+https://git.example.com/platform/devbox-profile.git --ref main --path profile.json
devbox profile add https://example.com/devbox/profile.json \
  --trust-issuer https://issuer.example.com --trust-identity https://ci.example.com/release
devbox profile list                 # ID, version, origin (system or user), source, state; --json
devbox profile show example-team    # settings with lock, credentials, groups, repositories
devbox profile update [ID]          # fetch remote profiles again
devbox profile remove example-team  # only profiles of the user file; deletes the cached copy
```

`add` fetches and checks the profile first and writes the source into `profiles` of the user
file only when that worked. A second profile with the same ID is refused.

**Signature.** With `--trust-issuer` and `--trust-identity` (`trust` in `config.json`), the
profile must be signed with Sigstore by that identity. The bundle lies next to the profile as
`<profile>.sigstore.json` (for example `profile.json.sigstore.json` in the same folder of the
repository or on the web server) and is checked with `cosign verify-blob`; `cosign` must be
in `PATH`. A missing or invalid signature is refused with the message of cosign, and no new
copy replaces the cached one. Local files with `trust` are checked at every start.

The web UI shows the profiles under Settings, with add, fetch again and remove; the API has
`GET /profiles`, `POST /profiles`, `POST /profiles/update` and `DELETE /profiles/{id}`.

## Credentials

A credential instance has a type (defined by a plugin: `registry` and `git-host` are bundled), fields, a secret
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

### Consumers

| Plugin | Type | Writes | Options of `--consumer` |
| --- | --- | --- | --- |
| `npm` | `registry` | `.npmrc`, or the environment (`npm.mode`) | `repository` or `url`, `scopes` |
| `maven` | `registry` | `settings.xml` | `mirror`, `releases`, `snapshots` |
| `gradle` | `registry` | `~/.gradle/gradle.properties`, or `ORG_GRADLE_PROJECT_*` in `env.d` (`gradle.mode`) | `repository` or `url`, `name` |
| `nuget` | `registry` | package sources and credentials in `NuGet.Config`, or `NuGetPackageSourceCredentials_*` in `env.d` (`nuget.mode`) | `repository` or `url`, `sourceName` |
| `docker` | `registry` | the login for the host in `~/.docker/config.json` (mode 0600) | `host` |
| `pip` | `registry` | `pip.conf`, `uv.toml` and `.netrc`, or `PIP_INDEX_URL` and `UV_DEFAULT_INDEX` in `env.d` (`pip.mode`, `pip.targets`) | `repository` or `url` |
| `git` | `git-host` | a credential helper entry for the host in `~/.gitconfig`; the token lies in a DevBox file with mode 0600 (`git.credentialHelper`) | `protocol` (`https`) |

Every consumer writes a marked block (or its own entries), keeps a backup and removes exactly
what it wrote: after `cred remove` the files are as before. With `repository`, the address
is built from the layout of the instance (`nexus`, `artifactory`); `url` gives the full
address. In mode `env` the secret lies only in the `env.d` file (mode 0600).

The `git` plugin also sets the identity and defaults:

```bash
devbox run git.identity -p name="M. Muster" -p email=m.muster@example.com
devbox run git.defaults                      # applies the object git.defaults with git config --global
```

## Cleanup

The `cleanup` plugin frees disk space. Every action asks for confirmation; with `--dry-run`
it lists what would go and how large it is, and deletes nothing.

```bash
devbox run cleanup.caches --dry-run           # npm, Maven, Gradle, NuGet, pip and uv caches with sizes
devbox run cleanup.projects --dry-run         # old build folders below cleanup.projectRoots
devbox run cleanup.docker                     # docker system prune, only with cleanup.docker = true
```

- `cleanup.projects` removes a folder only if it lies below a root, matches
  `cleanup.patterns` (`node_modules`, `target`, `build`, `dist`, …), is not tracked by Git
  and nothing in it changed within `cleanup.olderThanDays` (default 30). Symbolic links are
  neither followed nor removed.
- `cleanup.caches` selects the caches; the folder of each cache is a setting. The home
  directory, `/` and relative paths are refused as cache or project root.
- The check `cleanup.usage` warns when the caches are larger than `cleanup.warnAboveMb`.

## Project templates

The `templates` plugin creates projects from templates. A source is a Git repository with
one folder per template, each with a `template.json` (`template.v1.json`).

```bash
devbox run templates.sync                                     # fetch templates.sources into the cache
devbox status                                                 # templates.available lists the templates
devbox run templates.new -p template=node-service -p name=shop --dry-run
devbox run templates.new -p template=node-service -p name=shop -p values='{"port": 8080}'
devbox run templates.hooks -p template=node-service -p name=shop   # asks for confirmation
```

- `templates.new` creates `<templates.projectRoot>/<name>`, refuses a folder that is not
  empty and replaces `{{param}}` in the files the template marks for rendering.
- Commands a template brings along (`hooks`) never run on their own: `templates.new` only
  lists them, `templates.hooks` runs them after a confirmation.
- The plugin needs `git` and `jq` (both come with the base packages).

## Remote desktop (xrdp)

The `xrdp` plugin sets up xrdp with XFCE. All actions use sudo.

```bash
devbox run xrdp.install                       # xrdp, xorgxrdp and the desktop packages
devbox run xrdp.configure                     # port, security layer, colour depth, session start
devbox run xrdp.polkit                        # narrow polkit rule for remote sessions
devbox run xrdp.sound                         # sound module, with xrdp.sound = true
devbox run xrdp.enable                        # also: xrdp.restart, xrdp.disable (ask for confirmation)
```

- `xrdp.configure` changes only `port`, `security_layer` and `max_bpp` in `xrdp.ini` and
  `AllowRootLogin` in `sesman.ini`; every other line stays byte for byte, and both files
  are backed up first. The desktop start is a marked block in `~/.xsession`.
- `xrdp.allowRootLogin` is `false` by default, so the first `xrdp.configure` stops root
  from logging in over RDP (the stock configuration allows it).
- The polkit rule allows exactly the actions in `xrdp.polkitActions` for the user (or
  `xrdp.polkitGroup`); `xrdp.polkit = false` removes it.

## Dry run and backups

- `--dry-run` sets `DEVBOX_DRY_RUN=1` for the plugin. It still reads, checks and
  downloads metadata, but changes nothing and reports every change it would make.
- Plugins edit existing files through `devbox sdk block`, `devbox sdk json-set` (`-` as value
  reads it from standard input, `--mode 600` for a new file with secrets) and
  `devbox sdk json-remove`.
  Before the first change of a file in a session the original is copied to
  `~/.local/state/devbox/backups/<session>/<path>`; `devbox logs SESSION --json` lists the
  backups of a session.
- Changes to shell files are marked blocks (`# >>> devbox:<plugin> >>>` …
  `# <<< devbox:<plugin> <<<`), replaced in place on the next run instead of appended.
- Actions are idempotent: a second run with the same values changes nothing.

## Checks, actions and groups

```bash
devbox status [--strict]              # every check of every ready plugin, one line each
devbox check [ID ...] [--strict]      # the same with details and hints; ID is <plugin>.<check> or a plugin
devbox actions [PLUGIN]               # actions with their parameters, defaults and choices
devbox group list                     # action groups, for example update-all and cleanup-all
devbox group run update-all [--dry-run] [--yes]
```

- With `--strict`, `status` and `check` exit with 5 when a check warns and with 6 when one
  reports `error` or `missing`, so they can gate scripts and CI.
- A group is a sequence of actions. Ready plugins append their actions
  (`contributes.groups`); a profile that defines a group of the same ID replaces them.
- `group run` runs the actions one after another, each as its own session in the
  history. An action that fails or is not confirmed does not stop the others, and an
  action of a plugin that is not ready is skipped; the exit code is 1 when any failed.

## First-run setup

`devbox setup` runs the steps that bring a fresh machine into shape, in order.

```bash
devbox setup --plan                   # the steps with their state: done, open or blocked; --json
devbox setup --dry-run                # run every open step as a dry run
devbox setup --yes                    # run the open steps without asking
devbox setup --profile https://example.com/devbox/profile.json --yes   # add a profile first
```

- **Plan.** A profile with a `setup` list defines the steps and their order. Without one,
  the ready plugins contribute steps (`contributes.setup`), each after the steps it names
  in `after` (for example `node.install` after `base.packages`). `xrdp` contributes
  nothing: a remote desktop is set up only when a profile lists its actions.
- **Resume.** A step is done when the history holds a successful run of its action. A
  step that fails stops the run; the next `devbox setup` continues there and does not
  repeat finished steps.
- **Credentials.** A step `credentials:<name>` is done when the instance has a secret.
  `setup` does not ask for secrets; it names the command (`devbox cred set <name>`).
- **Exit codes.** 0 when every step that can run has run (also while secrets are
  missing), the code of the action that failed, 3 when a step is blocked (its plugin is
  missing or not ready), 4 without `--yes` and without a terminal.

The web UI shows the same plan on the page "Setup" (`/einrichtung`): every step with its
state, a button that starts the next open step, and the action groups with one button per
action. The API has `GET /setup` and `GET /groups`; a client runs steps and groups by
starting the actions with `POST /sessions`.

## Self-test and tools

```bash
devbox doctor [--offline]             # schemas, configuration, plugins, vault, service, proxy, network
devbox schema list                    # names of the embedded JSON Schemas
devbox schema print profile           # one schema, for editors and validation
devbox completion bash|zsh|fish       # completion script, e.g. source <(devbox completion bash)
```

`devbox doctor` changes nothing. It exits with 1 only for an `error` line; `info` and `warn`
lines (service not running, network not reachable) do not fail it. The network test sends
one request to `system.releaseUrl`.

## Exit codes

| Code | Meaning                                                                    |
| ---- | -------------------------------------------------------------------------- |
| 0    | success, also when there was nothing to do; `devbox status` exits 0 without `--strict` |
| 1    | error                                                                      |
| 2    | invalid parameter, setting, configuration, or unknown plugin or action     |
| 3    | a requirement is missing (plugin `blocked`, the action reported it, or `git` or the network is missing for a repository) |
| 4    | confirmation refused (or no terminal to ask and no `--yes`)                |
| 5    | `status --strict` or `check --strict`: a check warns                       |
| 6    | `status --strict` or `check --strict`: a check reports `error` or `missing` |
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
`repository`). Repositories of kind `plugins` and `catalog` (see
[Plugin catalogs](#plugin-catalogs)) work so far; entries of the other kinds (`profile`,
`templates`, `packages`) are listed with the state `unsupported` and otherwise ignored.

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
| `credential` | Name of a credential instance of type `git-host` or `registry` for a private repository, see below                    |
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

`repo add` takes `--source`, `--ref`, `--path`, `--credential NAME` and
`--trust prompt|pinned`. Plugins are the only kind so far, so there is no `--kind`.

`repo set NAME` changes an existing entry of the user file with the same flags, all
optional; only the flags that are given change something, and an empty value removes
`--ref`, `--path` or `--credential` (`--path ''`). The result is validated like a new entry, then the
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

A `trust` object (Sigstore signature) is not supported yet and is refused with an error; it
follows later.

### Private sources

`credential` names a credential instance (see "Credentials") of type `git-host` or
`registry` for a source that needs a login: a private Git repository over `git+https://` or
a tar.gz behind a login over `https://`.

```bash
devbox cred add company-git --type git-host --field url=https://git.example.com --field flavor=gitlab
devbox cred set company-git                       # hidden input; verifies the token
devbox repo add team-plugins --source git+https://git.example.com/platform/devbox-plugins.git \
  --path plugins --credential company-git
devbox repo set team-plugins --credential ''      # back to a public source
```

- The instance must exist and be of type `git-host` or `registry` when the entry is written;
  its secret is read from the vault at each sync. A missing secret or vault entry is an
  error in the state of the source, as is a vault that asks for a passphrase when the sync
  runs in the web service (`devbox serve` cannot ask; run `devbox repo sync` in a terminal).
- Only `git+https://` and `https://` sources take a credential. `git+ssh://` is refused
  (SSH keys are a separate credential type that follows later), as are `git+file://` and
  `file://`. An address must not contain a password; a user name next to a credential is
  refused as well.
- The secret goes only to the host of the instance (its field `url`) or a subdomain of it
  (`api.github.com` for `github.com`), only over https, and never to plugins. If the host
  of the source differs, nothing is sent and the sync fails with an error that names both
  hosts. A redirect to another host does not carry the secret.
- Downloads (`https://`) send the secret as the type defines it (`httpAuth` in
  `plugin.json`): `Authorization: Bearer`, `token` or `Basic`, or `PRIVATE-TOKEN` (GitLab).
  For a `registry` this follows the layout and the user name, for a `git-host` the flavor.
- Git (`git+https://`) gets the secret from a credential helper that is the `devbox` binary
  itself (`devbox repo git-credential`, hidden). DevBox writes the user name and the secret
  into a file (mode 0600) in a private directory (0700, below `XDG_RUNTIME_DIR` when there
  is one), starts Git with `-c credential.helper=` (which resets the helpers of the user,
  so none of them interferes) and the helper, and removes the directory after the sync.
  The environment of Git names the file (`DEVBOX_GIT_CREDENTIAL_FILE`), the secret is in no
  argument and no environment variable, and the helper answers only for the host of the
  instance. The user name is the field `username` of the instance (default `oauth2`).
- The secret is also removed from every error text (Git echoes addresses), so it is in no
  log, no `state.json`, no API answer and no event.

### Where the files live

Each revision is stored under `~/.local/share/devbox/repositories/<name>/<revision>/`
(the first 12 characters of the revision). `state.json` next to them
(`schema/repository-state.v1.json`) records the active and the pending revision, the last
sync and the last error. Syncing again with the same revision changes nothing. Plugins
are only read from the active revision; a pending one is not executed.

### Web UI and API

The REST API has `GET /repositories`, `POST /repositories`, `PATCH /repositories/{name}`
(any of `source`, `ref`, `path`, `credential`, `trust`; `null` removes `ref`, `path` and `credential`; answers with the
synced repository, `409` for an entry of the system file or a profile),
`POST /repositories/{name}/sync`, `POST /repositories/{name}/approve`,
`DELETE /repositories/{name}/pending` and `DELETE /repositories/{name}`. A missing `git` or
network answers `424` with the problem code `prerequisite`. After a sync, an approval or a switch, the WebSocket topic `plugins` sends
a `plugins` message with the changed plugin IDs and repositories.

In the web UI the "Sources" tab of the Plugins page lists the repositories. "Add source"
and "Edit" (sources of the user file) open the same form; the name cannot be changed. The
optional "Access" select lists the credential instances of type `git-host` and `registry`
(from `GET /credentials`). When
the newest revision of a source holds no plugins, a warning offers one button "Use path
<folder>" per suggested folder, which sets the path and syncs.

### Plugin catalogs

A catalog is a repository of kind `catalog` whose source is an `index.json`
(`catalog.v1.json`, `https://` or `file://`) that lists plugins with versioned, signed
packages. A catalog needs `trust` with the Sigstore identity that signs its packages:

```json
{
  "repositories": {
    "team-catalog": {
      "kind": "catalog",
      "source": "https://catalog.example.com/index.json",
      "trust": { "issuer": "https://issuer.example.com", "identity": "https://ci.example.com/release" }
    }
  }
}
```

```bash
devbox repo sync team-catalog            # fetch the index (also done on first use)
devbox plugin search [QUERY]             # ID, newest version, catalog, installed version; --json
devbox plugin install kubectl[@1.2.3]    # --catalog NAME when several catalogs offer it
devbox plugin update [ID]                # newest stable release of installed catalog plugins
devbox plugin remove kubectl
```

Installing downloads the package, checks its SHA-256 against the index and its Sigstore
bundle with `cosign verify-blob` against the identity of the catalog. A package with a
wrong digest, without bundle or signed by another identity is refused. Then DevBox lists
the permissions the plugin declares and asks for confirmation (`--yes` accepts them; without
a terminal and without `--yes` the call ends with exit code 4). An update asks again only
when the new version declares new permissions (marked `+`). Installed plugins live in
`~/.local/share/devbox/catalog/<id>/` and are recorded in
`~/.local/share/devbox/plugins.lock.json` (catalog, version, digest, signer). `plugin list`
shows them with the origin `catalog:<name>@<version>`.

In the web UI the Plugins page has a "Catalog" tab with search, install (after a dialog
with the permissions), update and remove; the API has `GET /catalog/search?q=`,
`POST /catalog/install` (without `confirm` it only returns the permissions) and
`DELETE /plugins/{id}`.

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
groups and setup.

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
- `devbox service …`, `devbox check --refresh` (the command line has no check cache)
- SSH key upload for `git-host` and the credential types `ssh-key`, `generic`; the
  event `credential.changed`
- version ranges from `plugins.require` of a profile (installing what a profile asks for)
- plugin repositories: the kinds `profile`, `templates` and `packages`, SSH keys for
  `git+ssh://` sources, Sigstore signatures (`trust` object) for kind `plugins`,
  `devbox repo init`
- `devbox migrate`
- global flags `--quiet`, `--no-color`, `--lang`, `--socket`
- the language packs `lang-de` and `lang-en`, rootless Docker, the keyring setup of `xrdp` and plugin templates for TypeScript, Python and Go
