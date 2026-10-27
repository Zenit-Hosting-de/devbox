# DevBox – Konzept und Umsetzungsplan für das Open-Source-Projekt von Zenit-Hosting

Oct 27, 2026 · @Kian

## Kurzfassung

**DevBox** richtet Linux-Rechner für die Softwareentwicklung ein und hält sie aktuell: Laufzeiten (Node.js, Java, .NET, Python), Werkzeuge, Git, Zugänge zu Paket-Registries, Docker, Remote-Desktop und Aufräumen. Bedient wird es über eine Web-Oberfläche, eine CLI oder ein Terminal-Menü. Ziel ist ein offenes Projekt unter `github.com/zenit-hosting/devbox`, das jedes Team mit eigenen Servern, eigenen Registries und eigenen Plugins nutzen kann, ohne eine Zeile im Kern zu ändern.

Dieses Dokument ist die Vorlage für die Umsetzung. Es ist so geschrieben, dass Claude Code daraus Arbeitspaket für Arbeitspaket bauen kann: jede Datei hat einen Pfad, ein Schema und ein Beispiel, jede Schnittstelle ein Format, jedes Paket Akzeptanzkriterien.

Die sechs Grundentscheidungen:

1. **Ein Go-Binary** `devbox` mit eingebetteter Angular-Oberfläche. Keine Laufzeit auf dem Zielrechner nötig außer der Shell.
2. **Alles ist ein Plugin.** Der Kern kennt keine Sprache, keine Registry und keinen Server. Er liest Manifeste und führt sie aus.
3. **Nichts ist fest eingebaut.** Jede Adresse, jeder Pfad, jede Version und jeder Schalter kommt aus der Konfiguration. Teams verteilen ihre Werte als Profil.
4. **JSON überall.** Konfiguration, Profile, Manifeste, Protokoll, API, Übersetzungen, Verlauf. JSON Schema 2020-12 ist die Quelle der Wahrheit, Typen für Go und TypeScript werden daraus erzeugt. Kein YAML, kein TOML, kein INI im Projekt selbst.
5. **Ohne Root, wo es geht.** Laufzeiten landen im Home-Verzeichnis. `sudo` nur für Schritte, die es wirklich brauchen, angekündigt und einzeln bestätigt.
6. **Geheimnisse nie im Klartext in DevBox-Dateien.** Tokens liegen im Schlüsselbund des Systems oder in einer verschlüsselten Datei. Plugins bekommen sie nur, wenn sie das Recht dafür angemeldet haben.

## Name und Auftritt

Das Projekt heißt **DevBox**, Herausgeber ist die GitHub-Organisation **zenit-hosting**. Der Befehl heißt `devbox`, im Text immer „DevBox“ mit großem B.

| Wo | Name |
| --- | --- |
| Repository Kern | `github.com/zenit-hosting/devbox` |
| Repository Plugin-Katalog | `github.com/zenit-hosting/d``evbox-plugins` |
| Go-Modul | `github.com/zenit-hosting/devbox` |
| npm-Paket für Plugin-Autoren | `@zenit-hosting/devbox-sdk` |
| Paket für apt und dnf | `devbox` (aus der eigenen Paketquelle) |
| Webseite und Doku | Vorschlag `devbox.zenit-hosting.de` |
| Konfiguration | `~/.config/devbox/`, Daten `~/.local/share/devbox/`, Zustand `~/.local/state/devbox/` |
| Umgebungsvariablen | Präfix `DEVBOX_` |

**Namenskonflikt.** Das Werkzeug *Jetify devbox* (Nix-basierte Entwicklungsumgebungen) installiert ebenfalls einen Befehl `devbox`, und die Namen `devbox` auf npm und PyPI sind vergeben. Drei Regeln halten das sauber:

- Das Installationsskript prüft, ob schon ein fremdes `devbox` im `PATH` liegt (erkennbar daran, dass `devbox version --json` kein Feld `"vendor": "zenit-hosting"` liefert). Dann wird der Befehl als `zdevbox` installiert und das in der Ausgabe gesagt. Der Name ist außerdem über `DEVBOX_BIN_NAME` wählbar.
- Veröffentlichte Pakete tragen immer den Scope oder das Präfix `zenit-hosting`, nie den nackten Namen.
- README und Webseite nennen den Unterschied in einem Satz, damit Suchende nicht im falschen Projekt landen.

**Begriffe**, die im ganzen Projekt gleich verwendet werden:

| Begriff | Bedeutung |
| --- | --- |
| Kern | das Go-Binary `devbox` mit API, Plugin-Host, Runner, Tresor und Konfiguration |
| Plugin | ein Ordner mit `plugin.json` und Skripten, der Fähigkeiten beisteuert |
| Prüfung | ein schneller, lesender Aufruf, der einen Zustand als JSON meldet |
| Aktion | ein Schritt, der etwas ändert und im Terminal läuft |
| Einstellung | ein Wert, den ein Plugin anmeldet und den Profile oder Benutzer setzen |
| Zugangstyp | eine Art Zugangsdaten mit Feldern, z. B. `registry` oder `git-host` |
| Abnehmer | ein Plugin, das Zugangsdaten eines Typs in Werkzeug-Konfigurationen schreibt |
| Profil | eine JSON-Datei mit Vorgaben eines Teams: Adressen, Versionen, Plugins |
| Sitzung | ein laufender oder beendeter Aktionslauf mit Ausgabe und Ergebnis |
| Gruppe | eine benannte Folge von Aktionen, z. B. „Alles aktualisieren“ |

## Ziele und Nicht-Ziele

**Ziele für Version 1.0**

- Ein frischer Rechner (Ubuntu, Debian, Fedora, Arch) ist mit einem Befehl und einem Profil in unter 15 Minuten arbeitsbereit.
- Jeder Schritt ist wiederholbar: zweimal ausgeführt ergibt denselben Zustand, ohne doppelte Einträge in Dateien.
- Jeder ändernde Schritt hat einen Probelauf (`--dry-run`), der genau sagt, was passieren würde.
- Die Übersicht zeigt in unter einer Sekunde den Zustand aller Plugins, auch ohne Netz.
- Ein neues Werkzeug lässt sich als Plugin in einer beliebigen Sprache in unter einer Stunde einbinden, ohne den Kern anzufassen.
- Teams verteilen ihre Vorgaben als ein Profil (eine JSON-Datei oder ein Git-Repository) und ändern nichts am Code.
- Oberfläche, CLI und Terminal-Menü können dasselbe, weil alle drei dieselbe API nutzen.

| Zielgruppe | Nutzen |
| --- | --- |
| Entwicklerinnen und Entwickler | Rechner einrichten, Laufzeiten wechseln, Tokens erneuern, aufräumen, ohne Anleitungen zu suchen |
| Teams und Plattform-Verantwortliche | einheitliche Rechner über ein Profil, eigene Plugins für interne Werkzeuge |
| Betrieb von Entwicklungs-VMs | Remote-Desktop, Dienste, Updates und Aufräumen ohne Root-Shell |
| Plugin-Autorinnen und -Autoren | klares Protokoll, SDK, Katalog zum Teilen |

**Nicht-Ziele**

- Kein Ersatz für Ansible, Nix oder Dotfile-Manager. DevBox richtet den Arbeitsplatz eines Menschen ein, keine Serverflotten.
- Keine Container-Umgebungen pro Projekt (das machen Dev Containers und Jetify devbox gut).
- Kein Windows und kein macOS in 1.0. Die Architektur verbaut es nicht, aber Plugins dürfen in 1.0 Linux voraussetzen.
- Keine zentrale Verwaltung vieler Rechner, kein Server-Dienst, keine Telemetrie.
- Keine eigene Paketverwaltung für Laufzeiten: Plugins nutzen offizielle Quellen und Werkzeuge (Adoptium, dotnet-install, uv, Node-Releases).

## Grundprinzipien

Diese Regeln gelten für jede Zeile Code und jedes Plugin. Ein Review darf jede Änderung ablehnen, die eine davon bricht.

| Prinzip | Was es konkret heißt |
| --- | --- |
| Alles konfigurierbar | Kein Hostname, keine URL, kein Registry-Name, keine Version und kein Pfad steht fest im Code. Jeder Wert ist eine Einstellung mit Standardwert im Manifest. Standardwerte zeigen nur auf öffentliche Quellen (nodejs.org, api.adoptium.net, registry.npmjs.org) oder sind leer. |
| Profile statt Forks | Was ein Team anders will, steht in einem Profil. Ein Fork des Kerns für eigene Adressen ist ein Fehler im Konzept. |
| JSON überall | Jede Datei, die DevBox liest oder schreibt, ist JSON oder JSON Lines und hat ein Schema unter `schema/`. Fremde Formate (`.npmrc`, `settings.xml`, `pip.conf`, `NuGet.Config`) schreiben nur Plugins, weil die Werkzeuge sie verlangen. |
| Streng lesen | Unbekannte Schlüssel sind ein Fehler mit Pfad und Zeile, außer `$schema` und `$comment`. Jede Datei trägt `"$schema"` mit Version, damit Editoren Vervollständigung bieten. |
| Plugins statt Kernlogik | Der Kern kennt keine Sprache und kein Werkzeug. Braucht ein Plugin etwas, das es nicht gibt, wird das Protokoll erweitert, nicht der Kern um Sonderfälle. |
| Wiederholbar | Jede Aktion prüft vorher und ändert nur, was abweicht. Dateien werden in markierten Blöcken (`# >>> devbox:<plugin> >>>`) gepflegt, nie blind angehängt. |
| Probelauf zuerst | Jede Aktion unterstützt `DEVBOX_DRY_RUN=1` und meldet geplante Änderungen als Nachrichten vom Typ `change`. |
| Ohne Root | Root nur über angekündigte Schritte mit `sudo`, nie für den Dienst selbst. Der Dienst läuft als Benutzer. |
| Offline zuerst | Prüfungen arbeiten lokal. Netzabfragen (neueste Versionen) laufen getrennt und werden zwischengespeichert. |
| Nachvollziehbar | Jede Sitzung landet mit Ausgabe, Ergebnis und geänderten Dateien im Verlauf. Vor jeder Änderung an einer Datei wird eine Sicherung angelegt. |
| Neutral formuliert | Texte in Oberfläche und Ausgabe sprechen niemanden an (kein du, kein Sie, kein ich): „Token gespeichert“, „Version wählen“. |

## Architektur

Ein einziges Programm `devbox` ist Dienst, CLI und Terminal-Menü zugleich. Als Dienst läuft es als systemd-Benutzerdienst (`devbox.service` unter `~/.config/systemd/user/`) und liefert die Oberfläche aus. Die CLI spricht mit dem laufenden Dienst über einen Unix-Socket; läuft keiner, arbeitet sie direkt mit denselben Go-Paketen. Plugins sind getrennte Prozesse, die der Kern startet. Er lädt nie fremden Code in den eigenen Prozess.

| Baustein | Go-Paket | Aufgabe |
| --- | --- | --- |
| API | `internal/api` | HTTP und WebSocket, Anmeldung, TLS, statische Oberfläche |
| Plugin-Host | `internal/plugin` | Manifeste finden, prüfen, Rechte und Abhängigkeiten auflösen, Ereignisse verteilen |
| Runner | `internal/runner` | Prüfungen und Aktionen starten, PTY, Zeitüberschreitung, Abbruch, Sitzungen |
| Konfiguration | `internal/config` | Ebenen zusammenführen, Platzhalter auflösen, gegen Schemas prüfen |
| Profile | `internal/profile` | Profile aus Datei, URL oder Git holen, Signatur prüfen, zwischenspeichern |
| Tresor | `internal/vault` | Geheimnisse im Schlüsselbund oder in einer age-Datei, Ablaufdaten |
| Verlauf | `internal/history` | Sitzungen als JSON Lines, Sicherungen geänderter Dateien |
| Schemas | `internal/schema` | eingebettete JSON-Schemas, Prüfung mit Pfadangabe |
| Katalog | `internal/catalog` | Plugins suchen, installieren, aktualisieren, Signaturen prüfen |

Datenfluss am Beispiel „Java aktualisieren“:

1. Oberfläche oder CLI ruft `POST /api/v1/sessions` mit `{"action": "java.update"}` auf.
2. Der Plugin-Host findet die Aktion im Manifest von `java`, prüft Rechte und ob alle Einstellungen gesetzt sind.
3. Die Konfiguration liefert die aufgelösten Einstellungen des Plugins, der Tresor nur die angemeldeten Geheimnisse.
4. Der Runner startet das Skript in einem PTY mit Umgebung und Arbeitsverzeichnis, die Ausgabe geht über WebSocket an alle Zuschauer.
5. Strukturierte Nachrichten des Skripts (Fortschritt, Änderungen, Ergebnis) landen im Verlauf, danach läuft die Prüfung `java.installed` neu und die Übersicht aktualisiert sich.

&#91;embedded content: Architektur · Oberflächen, Kern, Plugins\]

## Technik-Entscheidungen

Jede Entscheidung hat einen Grund. Wer sie ändern will, schreibt einen ADR unter `docs/adr/` (Markdown, nummeriert), bevor Code entsteht.

| Bereich | Wahl | Grund |
| --- | --- | --- |
| Kern | Go 1.23 oder neuer, ein statisches Binary (`CGO_ENABLED=0`) | läuft auf jeder Distribution ohne Laufzeit, schneller Start, einfache Releases |
| CLI | `spf13/cobra` | Standard für Go-CLIs, Shell-Vervollständigung inklusive |
| HTTP | `net/http` mit dem Router aus der Standardbibliothek | keine Abhängigkeit für Routen mit Methoden und Platzhaltern |
| WebSocket | `github.com/coder/websocket` | gepflegt, kontextbasiert, ohne Altlasten |
| Terminal | `github.com/creack/pty` | echtes PTY für Farben, Fortschrittsbalken und `sudo`-Abfragen |
| Logging | `log/slog` mit JSON-Handler | strukturiert, Standardbibliothek |
| Schemas prüfen | `github.com/santhosh-tekuri/jsonschema/v6` | vollständige Unterstützung für JSON Schema 2020-12, gute Fehlerpfade |
| Typen erzeugen | `go-jsonschema` für Go, `json-schema-to-typescript` für die Oberfläche | eine Quelle, keine auseinanderlaufenden Typen |
| Geheimnisse | `github.com/zalando/go-keyring` (Secret Service), ersatzweise `filippo.io/age` | Schlüsselbund des Desktops, verschlüsselte Datei auf Servern ohne Desktop |
| Anmeldung | PAM über `unix_chkpwd`, Sitzung als HttpOnly-Cookie | kein eigenes Passwort, keine cgo-Abhängigkeit |
| Terminal-Menü | `charmbracelet/bubbletea` mit `bubbles` | läuft per SSH, gleiche Daten wie die Oberfläche |
| Oberfläche | Angular (aktuelle LTS), Signals, Standalone-Komponenten, zoneless | moderner Stand, keine Module, wenig Abhängigkeiten |
| Komponenten | `zenit-ui` (MIT) | einheitliches Aussehen, Themes und Barrierefreiheit fertig |
| Terminal im Browser | `@xterm/xterm` mit Fit- und Weblinks-Addon | Standard für Terminals im Browser |
| Plugin-Skripte | beliebige Sprache, Vorlage in Bash mit `shellcheck` | Werkzeuge auf Linux sind Shell, Plugins sollen ohne Build auskommen |
| Pakete | GoReleaser mit `nfpm` für `.deb` und `.rpm`, AUR-Paket, `install.sh` | ein Release-Lauf erzeugt alles |
| Signaturen | Sigstore `cosign` (schlüssellos über GitHub OIDC) | Releases und Plugins prüfbar ohne eigene Schlüsselverwaltung |
| Tests | `go test`, `testscript` für CLI-Abläufe, Vitest für Angular, Playwright für Ende-zu-Ende, `bats` für Plugins | jede Ebene mit dem passenden Werkzeug |

## Konfiguration

Jeder Wert, den ein Plugin braucht, ist eine **Einstellung** mit einem Schlüssel der Form `<plugin>.<name>`, zum Beispiel `node.channel`, `java.vendorApi` oder `npm.scopes`. Das Plugin meldet sie im Manifest an, mit Typ, Standardwert, Beschriftung und Hilfetext. Damit erscheinen sie automatisch in Oberfläche, CLI und Terminal-Menü.

**Ebenen.** Der wirksame Wert entsteht aus sechs Ebenen. Die spätere gewinnt:

| Rang | Ebene | Quelle | Gedacht für |
| --- | --- | --- | --- |
| 1 | Plugin-Standard | `default` in `plugin.json` | öffentliche Quellen, sinnvolle Grundwerte |
| 2 | System | `/etc/devbox/config.json` | Images, vorinstallierte VMs, Administratoren |
| 3 | Profile | Dateien aus `profiles` in `config.json`, in Reihenfolge | Vorgaben eines Teams oder Unternehmens |
| 4 | Benutzer | `settings` in `~/.config/devbox/config.json` | eigene Anpassungen |
| 5 | Umgebung | `DEVBOX_SET__<PLUGIN>__<NAME>`, z. B. `DEVBOX_SET__NODE__CHANNEL=lts` | CI, Container, einmalige Läufe |
| 6 | Aufruf | `--set node.channel=lts` an der CLI | einzelner Befehl |

**Gesperrte Werte.** Ein Profil kann einzelne Schlüssel mit `"locked": true` setzen. Ebenen 4 bis 6 dürfen sie dann nicht überschreiben; die Oberfläche zeigt sie schreibgeschützt mit dem Namen des Profils. Damit kann ein Team etwa die Registry-Adresse festlegen, ohne dass sie versehentlich geändert wird.

**Platzhalter.** In Zeichenketten sind Verweise erlaubt, aufgelöst beim Laden, mit Erkennung von Zyklen:

| Platzhalter | Wert |
| --- | --- |
| `${node.channel}` | eine andere Einstellung |
| `${env:HOME}` | eine Umgebungsvariable |
| `${xdg:config}`, `${xdg:data}`, `${xdg:state}`, `${xdg:cache}` | XDG-Verzeichnisse des Benutzers |
| `${host:name}`, `${host:user}`, `${host:arch}`, `${host:distro}`, ${host:codename} | Angaben zum Rechner |
| `${registry:<instanz>.url}` | Feld einer Zugangsdaten-Instanz, ohne Geheimnis |

`$${` schreibt ein wörtliches `${`. Geheimnisse sind über Platzhalter nie erreichbar.

**Herkunft sichtbar.** `devbox config explain node.channel` zeigt den wirksamen Wert, die Ebene, aus der er stammt, und alle überschriebenen Werte darunter. Die Oberfläche zeigt dasselbe als Hinweis neben jedem Feld.

**Profile** sind der Weg, DevBox für ein Team einzurichten. Ein Profil kann:

- Einstellungen setzen und sperren,
- Plugins einschalten, abschalten oder aus dem Katalog mit Versionsbereich verlangen,
- Zugangsdaten-Instanzen vorbereiten (Name, Typ, Adresse), sodass nur noch das Token fehlt,
- Gruppen und die Reihenfolge der Ersteinrichtung festlegen,
- Oberfläche anpassen: Titel, Akzentfarbe, Links in der Kopfzeile, Hinweistext auf der Übersicht.

Quellen für Profile sind eine lokale Datei, eine HTTPS-Adresse oder ein Git-Repository mit Pfad und Ref. Entfernte Profile werden unter `~/.cache/devbox/profiles/` zwischengespeichert und mit `devbox profile update` erneuert. Optional verlangt die Konfiguration eine Sigstore-Signatur des Profils von einer bestimmten Identität.

## Dateien und Formate

Alle Pfade folgen der XDG-Spezifikation. Jede Datei hat ein Schema unter `schema/` im Repository, eingebettet ins Binary und veröffentlicht unter `https://devbox.zenit-hosting.de/schema/<name>.v1.json`. DevBox schreibt JSON immer mit zwei Leerzeichen Einrückung, Schlüsseln in der Reihenfolge des Schemas und abschließendem Zeilenumbruch, damit Diffs ruhig bleiben. Schreiben passiert atomar (temporäre Datei, `fsync`, `rename`).

| Datei | Pfad | Schema | Wer schreibt |
| --- | --- | --- | --- |
| Benutzer-Konfiguration | `~/.config/devbox/config.json` | `config.v1.json` | Benutzer, Oberfläche, CLI |
| System-Konfiguration | `/etc/devbox/config.json` | `config.v1.json` | Administratoren, Pakete |
| Profil | beliebig, z. B. `profile.json` in einem Team-Repository | `profile.v1.json` | Team |
| Plugin-Manifest | `<plugin>/plugin.json` | `plugin.v1.json` | Plugin-Autoren |
| Zugangsdaten-Verzeichnis | `~/.local/share/devbox/credentials.json` | `credentials.v1.json` | Kern (nur Metadaten, keine Geheimnisse) |
| Tresor-Datei | `~/.local/share/devbox/vault.age` | verschlüsseltes JSON nach `vault.v1.json` | Kern, nur ohne Schlüsselbund |
| Plugin-Sperrdatei | `~/.local/share/devbox/plugins.lock.json` | `lock.v1.json` | Kern beim Installieren aus dem Katalog |
| Verlauf | `~/.local/state/devbox/history/<JJJJ-MM>.jsonl` | `history-entry.v1.json` | Kern, eine Zeile je beendeter Sitzung |
| Sitzungsereignisse | `~/.local/state/devbox/sessions/<id>.events.jsonl` | `event.v1.json` | Kern, alle Nachrichten einer Sitzung |
| Sitzungsausgabe | `~/.local/state/devbox/sessions/<id>.log` | Rohtext mit ANSI | Kern |
| Sicherungen | `~/.local/state/devbox/backups/<id>/<pfad>` | Originaldateien | Kern vor jeder Änderung |
| Zwischenspeicher | `~/.cache/devbox/` (`checks.json`, `versions/`, `profiles/`, `catalog/`) | je eigenes Schema | Kern |
| Übersetzungen | `<plugin>/i18n/<sprache>.json`, `web/src/i18n/<sprache>.json` | `messages.v1.json` | Autoren |
| Katalog-Index | `index.json` im Katalog-Repository | `catalog.v1.json` | CI des Katalogs |

**`config.json`** – Benutzer-Konfiguration mit allen Bereichen:

```json
{
  "$schema": "https://devbox.zenit-hosting.de/schema/config.v1.json",
  "profiles": [
    { "source": "git+https://git.example.com/platform/devbox-profile.git", "ref": "main", "path": "profile.json" },
    { "source": "file://${xdg:config}/devbox/local-profile.json" }
  ],
  "plugins": {
    "paths": ["${xdg:data}/devbox/plugins", "/usr/share/devbox/plugins"],
    "disabled": ["xrdp"]
  },
  "settings": {
    "node.channel": "lts",
    "java.defaultVersion": 21
  },
  "repositories": {
    "zenit-hosting": {
      "kind": "catalog",
      "source": "https://github.com/zenit-hosting/devbox-plugins",
      "trust": { "issuer": "https://token.actions.githubusercontent.com", "identity": "https://github.com/zenit-hosting/devbox-plugins/.github/workflows/release.yml@refs/heads/main" }
    }
  },
  "server": { "listen": "127.0.0.1:7878", "auth": "local", "tls": { "cert": "", "key": "" }, "allowedOrigins": [] },
  "ui": { "language": "de", "scheme": "system", "accent": "blau" },
  "vault": { "backend": "auto" },
  "history": { "keepDays": 90, "keepSessions": 500 },
  "network": { "proxy": "${env:HTTPS_PROXY}", "noProxy": "localhost,127.0.0.1", "caFile": "" }
}
```

`server.auth` kennt `local` (nur Unix-Socket und `127.0.0.1`, ohne Login), `pam` (Login mit Systemkonto) und `off` (nur für Tests, startet nicht auf anderen Adressen als Loopback). `vault.backend` kennt `auto`, `keyring` und `age`.

**`profile.json`** – Vorgaben eines Teams. Alle Adressen sind Beispiele:

```json
{
  "$schema": "https://devbox.zenit-hosting.de/schema/profile.v1.json",
  "id": "example-team",
  "name": "Beispiel-Team",
  "version": "2026.10.1",
  "requires": { "devbox": ">=1.0.0 <2.0.0" },
  "plugins": {
    "require": { "kubectl": { "catalog": "zenit-hosting", "version": "^1.2.0" } },
    "disable": ["xrdp"]
  },
  "settings": {
    "node.channel": { "value": "lts" },
    "java.defaultVersion": { "value": 21 },
    "base.packages": { "value": ["git", "curl", "jq", "unzip", "build-essential"] },
    "cleanup.projectRoots": { "value": ["${env:HOME}/projekte"] }
  },
  "credentials": {
    "firmen-registry": {
      "type": "registry",
      "label": "Firmen-Registry",
      "fields": { "url": "https://nexus.example.com", "layout": "nexus" },
      "locked": ["url", "layout"],
      "consumers": {
        "npm": { "repository": "npm-group", "scopes": ["@example"] },
        "maven": { "mirror": "maven-public", "releases": "maven-releases", "snapshots": "maven-snapshots" },
        "nuget": { "repository": "nuget-group" },
        "pip": { "repository": "pypi-group" },
        "docker": { "host": "docker.example.com" }
      }
    },
    "firmen-git": {
      "type": "git-host",
      "label": "Firmen-Git",
      "fields": { "url": "https://git.example.com", "flavor": "gitlab" },
      "consumers": { "git": { "protocol": "https" } }
    }
  },
  "groups": {
    "update-all": { "label": "Alles aktualisieren", "actions": ["base.upgrade", "node.update", "java.update", "dotnet.update", "python.update"] }
  },
  "setup": ["base.packages", "git.identity", "node.install", "java.install", "credentials:firmen-registry", "credentials:firmen-git"],
  "branding": {
    "title": "DevBox",
    "accent": "blau",
    "notice": "Fragen zur Einrichtung im Kanal #devbox.",
    "links": [{ "label": "Wiki", "url": "https://wiki.example.com/devbox" }]
  }
}
```

Ein Abnehmer akzeptiert je Repository entweder einen Namen (`"repository": "npm-group"`), aus dem er mit dem `layout` der Instanz die Adresse baut (`nexus`: `<url>/repository/<name>/`, `artifactory`: `<url>/artifactory/api/npm/<name>/`), oder eine vollständige Adresse (`"url": "https://…"`). Damit funktioniert jede Registry, auch eine selbst gebaute mit `"layout": "custom"`.

**`credentials.json`** – nur Metadaten, das Geheimnis liegt im Tresor:

```json
{
  "$schema": "https://devbox.zenit-hosting.de/schema/credentials.v1.json",
  "instances": {
    "firmen-registry": {
      "type": "registry",
      "origin": "profile:example-team",
      "fields": { "url": "https://nexus.example.com", "layout": "nexus", "username": "m.muster" },
      "secret": { "ref": "keyring:devbox/firmen-registry", "updatedAt": "2026-10-27T09:12:00Z", "expiresAt": "2027-01-25T00:00:00Z" },
      "applied": { "npm": "2026-10-27T09:12:03Z", "maven": "2026-10-27T09:12:04Z" }
    }
  }
}
```

**Verlauf** – eine Zeile je beendeter Sitzung:

```json
{"id":"s_01JB8Y3K2M","action":"java.update","title":"Java aktualisieren","user":"m.muster","origin":"ui","dryRun":false,"startedAt":"2026-10-27T09:20:11Z","endedAt":"2026-10-27T09:21:40Z","state":"success","exitCode":0,"changes":2,"summary":"Temurin 21.0.5 installiert, JAVA_HOME gesetzt"}
```

**`plugins.lock.json`** – hält fest, was aus Katalogen installiert ist, damit derselbe Stand reproduzierbar ist:

```json
{
  "$schema": "https://devbox.zenit-hosting.de/schema/lock.v1.json",
  "plugins": {
    "kubectl": { "catalog": "zenit-hosting", "version": "1.2.3", "digest": "sha256:3f1c…", "signedBy": "https://github.com/zenit-hosting/devbox-plugins/.github/workflows/release.yml@refs/heads/main", "installedAt": "2026-10-27T09:00:00Z" }
  }
}
```

## Plugin-System

Ein Plugin ist ein Ordner mit einer `plugin.json` und beliebigen ausführbaren Dateien. Es gibt keinen Build-Schritt und keine Bibliothek, die ein Plugin einbinden muss. Alles, was DevBox über ein Plugin wissen muss, steht im Manifest; alles, was es tut, tun seine Skripte.

**Bausteine** eines Manifests:

| Baustein | Schlüssel | Zweck |
| --- | --- | --- |
| Prüfung | `checks` | liest einen Zustand und meldet `ok`, `warn`, `error` oder `missing` mit Text und Daten; schnell, ohne Änderungen |
| Aktion | `actions` | ändert etwas, läuft im PTY, hat Parameter, Probelauf und optional eine Rückfrage |
| Einstellung | `settings` | Werte, die das Plugin braucht, mit Typ und Standardwert (JSON-Schema-Ausschnitt) |
| Zugangstyp | `credentialTypes` | definiert eine Art Zugangsdaten mit Feldern, Geheimnis, Prüfaktion und Ablauf |
| Abnehmer | `consumes` | schreibt Zugangsdaten eines Typs in die Konfiguration eines Werkzeugs und entfernt sie wieder |
| Beiträge | `contributes` | hängt Aktionen an Gruppen, Ersteinrichtung, Übersichtskarten und Links an |
| Ereignisse | `on` | reagiert auf Ereignisse des Kerns, z. B. `credential.changed` oder `session.finished` |
| Voraussetzungen | `requires` | DevBox-Version, Betriebssystem, Architektur, Befehle im `PATH`, andere Plugins |
| Rechte | `permissions` | Netzziele, lesbare und schreibbare Pfade, `sudo`, Zugriff auf Zugangstypen |
| Sprachpaket | `contributes.language` | liefert alle Texte einer Sprache für Kern und Plugins; erscheint in der Sprachauswahl der Ersteinrichtung |

**Bezeichner.** Plugin-IDs passen auf `^[a-z][a-z0-9-]{1,31}$`. Prüfungen, Aktionen und Einstellungen werden überall voll qualifiziert angesprochen: `java.install`, `java.installed`, `java.defaultVersion`. Parameter und Einstellungen benutzen `lowerCamelCase`.

**Suche und Vorrang.** Der Kern liest Plugins aus `plugins.paths` in dieser Reihenfolge. Gibt es eine ID zweimal, gewinnt der erste Fund und die Übersicht zeigt einen Hinweis. So kann ein Team ein mitgeliefertes Plugin durch eine eigene Fassung ersetzen, ohne den Kern zu ändern. Mitgelieferte Plugins liegen unter `/usr/share/devbox/plugins` (Paket) oder im Binary eingebettet und werden beim ersten Start nach `~/.local/share/devbox/builtin/<version>/` entpackt.

**Zustände** eines Plugins: `invalid` (Manifest fehlerhaft, mit Fehlerpfad), `blocked` (Voraussetzung fehlt, mit Grund), `disabled` (abgeschaltet), `incomplete` (Pflicht-Einstellung leer) und `ready`. Nur `ready` erscheint mit Aktionen in der Oberfläche, alle anderen mit dem Grund und einem Weg zur Lösung.

**Vollständiges Beispiel** `plugins/java/plugin.json`:

```json
{
  "$schema": "https://devbox.zenit-hosting.de/schema/plugin.v1.json",
  "id": "java",
  "version": "1.0.0",
  "name": "Java",
  "description": "JDKs installieren, wechseln und aktuell halten",
  "category": "runtime",
  "icon": "coffee",
  "license": "Apache-2.0",
  "authors": ["Zenit-Hosting"],
  "homepage": "https://github.com/zenit-hosting/devbox/tree/main/plugins/java",
  "requires": {
    "devbox": ">=1.0.0",
    "os": ["linux"],
    "arch": ["amd64", "arm64"],
    "commands": ["tar"],
    "plugins": { "base": ">=1.0.0" }
  },
  "permissions": {
    "network": ["${java.vendorApi}", "https://github.com", "https://objects.githubusercontent.com"],
    "files": {
      "read": ["${env:HOME}"],
      "write": ["${java.installDir}", "${xdg:config}/devbox/env.d/java.sh"]
    },
    "sudo": false,
    "credentials": []
  },
  "settings": {
    "vendorApi": { "type": "string", "format": "uri", "default": "https://api.adoptium.net/v3", "label": "Adresse der Release-API", "help": "Adoptium-kompatible API. Für einen Spiegel im eigenen Netz ändern." },
    "defaultVersion": { "type": "integer", "enum": [8, 11, 17, 21, 25], "default": 21, "label": "Standard-JDK" },
    "installDir": { "type": "string", "format": "path", "default": "${xdg:data}/jdk", "label": "Installationsort", "advanced": true },
    "keepPatches": { "type": "integer", "minimum": 1, "maximum": 5, "default": 1, "label": "Patchstände je Version behalten" }
  },
  "checks": {
    "installed": { "label": "Installierte JDKs", "run": ["checks/installed.sh"], "timeout": "5s", "cache": "10m", "refreshOn": ["session.finished:java.*"] },
    "latest": { "label": "Neue Versionen", "run": ["checks/latest.sh"], "timeout": "20s", "cache": "6h", "network": true }
  },
  "actions": {
    "install": {
      "label": "JDK installieren",
      "run": ["actions/install.sh"],
      "params": {
        "version": { "type": "integer", "enum": [8, 11, 17, 21, 25], "default": "${java.defaultVersion}", "label": "Version" },
        "makeDefault": { "type": "boolean", "default": true, "label": "Danach als Standard setzen" }
      },
      "timeout": "15m"
    },
    "update": { "label": "Aktualisieren und aufräumen", "run": ["actions/update.sh"], "timeout": "30m" },
    "use": {
      "label": "Standard-JDK wechseln",
      "run": ["actions/use.sh"],
      "params": { "version": { "type": "string", "label": "Version", "optionsFrom": "java.installed#/data/items/*/version" } }
    },
    "remove": {
      "label": "JDK entfernen",
      "run": ["actions/remove.sh"],
      "params": { "version": { "type": "string", "label": "Version", "optionsFrom": "java.installed#/data/items/*/version" } },
      "confirm": "Das JDK wird gelöscht. Projekte, die es über JAVA_HOME nutzen, brauchen danach ein anderes.",
      "danger": true
    }
  },
  "contributes": {
    "groups": { "update-all": ["java.update"] },
    "setup": [{ "action": "java.install", "after": ["base.packages"] }],
    "overview": { "check": "java.installed", "primaryAction": "java.update" }
  },
  "i18n": { "default": "de", "dir": "i18n" }
}
```

`optionsFrom` füllt eine Auswahl aus den Daten einer Prüfung (JSON Pointer mit `*` für Listen). `confirm` als Text erzwingt eine Rückfrage mit genau diesem Text; `danger` färbt die Schaltfläche als gefährlich. `timeout` und `cache` sind Go-Dauern (`5s`, `10m`, `6h`).

**Zugangstyp und Abnehmer** am Beispiel einer Paket-Registry. Das Plugin `registry` definiert den Typ, `npm` schreibt ihn in `.npmrc`:

```json
{
  "id": "registry",
  "credentialTypes": {
    "registry": {
      "label": "Paket-Registry",
      "fields": {
        "url": { "type": "string", "format": "uri", "label": "Adresse" },
        "layout": { "type": "string", "enum": ["nexus", "artifactory", "gitea", "gitlab", "custom"], "default": "nexus", "label": "Art der Registry" },
        "username": { "type": "string", "label": "Benutzername" }
      },
      "secret": { "label": "Token oder Passwort", "expires": "optional" },
      "verify": "registry.verify"
    }
  }
}
```

```json
{
  "id": "npm",
  "consumes": {
    "registry": {
      "apply": ["consume/apply.sh"],
      "remove": ["consume/remove.sh"],
      "options": {
        "repository": { "type": "string", "label": "Repository-Name" },
        "url": { "type": "string", "format": "uri", "label": "Vollständige Adresse" },
        "scopes": { "type": "array", "items": { "type": "string", "pattern": "^@[a-z0-9-~][a-z0-9-._~]*$" }, "label": "Scopes" }
      }
    }
  },
  "permissions": { "credentials": ["registry"], "files": { "write": ["${env:HOME}/.npmrc"] } }
}
```

Wird ein Token gespeichert oder erneuert, löst der Kern `credential.changed` aus und ruft `apply` aller Abnehmer, die für diese Instanz eingetragen sind. Beim Löschen ruft er `remove`. Abnehmer sind damit voneinander unabhängig: ein neues Werkzeug (etwa Gradle oder Poetry) ist ein neues Plugin mit einem `consumes`-Eintrag, sonst nichts.

## Plugin-Protokoll

Das Protokoll hat die Version `1` und steht als `"protocol": 1` im Manifest (fehlt der Schlüssel, gilt 1). Erweiterungen sind abwärtskompatibel: neue Nachrichtentypen und Felder dürfen Plugins ignorieren, der Kern ignoriert unbekannte Felder in Nachrichten nicht, sondern protokolliert sie als Warnung.

**Aufruf.** `run` ist ein Array wie bei `exec`, kein Shell-String. Das erste Element ist relativ zum Plugin-Ordner oder ein Befehl im `PATH`. Arbeitsverzeichnis ist `$HOME`. Die Umgebung ist leer bis auf die Liste unten, `PATH`, `HOME`, `USER`, `LANG`, `TERM` und die Proxy-Variablen aus `network`.

| Variable | Inhalt |
| --- | --- |
| `DEVBOX_PROTOCOL` | `1` |
| `DEVBOX_VERSION` | Version des Kerns, z. B. `1.0.0` |
| `DEVBOX_PLUGIN_ID`, `DEVBOX_PLUGIN_DIR` | eigene ID und eigener Ordner |
| `DEVBOX_SESSION_ID` | ID der Sitzung, leer bei Prüfungen |
| `DEVBOX_REQUEST` | Pfad zur Anfrage als JSON (Modus 0600 unter `$XDG_RUNTIME_DIR/devbox/`, nach dem Lauf gelöscht) |
| `DEVBOX_MSG_FD` | Dateideskriptor für Nachrichten, immer `3` |
| `DEVBOX_DRY_RUN` | `1` im Probelauf, sonst `0` |
| `DEVBOX_YES` | `1`, wenn Rückfragen im Skript automatisch bejaht werden sollen |
| `DEVBOX_LANG` | Sprache der Oberfläche, z. B. `de` |
| `DEVBOX_STATE_DIR`, `DEVBOX_CACHE_DIR` | eigene Verzeichnisse des Plugins unter `~/.local/state/devbox/plugins/<id>` und `~/.cache/devbox/plugins/<id>` |
| `DEVBOX_BACKUP_DIR` | Ablage für Sicherungen dieser Sitzung |
| `DEVBOX_BIN` | Pfad zum `devbox`-Binary für die Hilfsbefehle |

**Anfrage** (`DEVBOX_REQUEST`):

```json
{
  "protocol": 1,
  "kind": "action",
  "plugin": "java",
  "name": "install",
  "params": { "version": 21, "makeDefault": true },
  "settings": { "vendorApi": "https://api.adoptium.net/v3", "defaultVersion": 21, "installDir": "/home/m.muster/.local/share/jdk", "keepPatches": 1 },
  "credential": null,
  "dryRun": false,
  "host": { "distro": "ubuntu", "distroVersion": "24.04", "arch": "amd64", "desktop": "xfce" }
}
```

Bei Abnehmern steht unter `credential` die Instanz mit Feldern, Optionen aus dem Profil und dem Geheimnis: `{ "instance": "firmen-registry", "type": "registry", "fields": {…}, "options": {…}, "secret": "…" }`. Geheimnisse erscheinen nur hier, nie in Umgebungsvariablen oder Kommandozeilen.

**Prüfungen** laufen ohne PTY. Sie schreiben genau ein JSON-Objekt auf `stdout`, Text auf `stderr` landet im Debug-Log:

```json
{
  "status": "warn",
  "summary": "Temurin 21.0.4 (Standard), 17.0.12",
  "data": { "items": [ { "version": "21.0.4", "path": "/home/m.muster/.local/share/jdk/21.0.4", "default": true }, { "version": "17.0.12", "path": "/home/m.muster/.local/share/jdk/17.0.12", "default": false } ] },
  "details": [ { "label": "JAVA_HOME", "value": "/home/m.muster/.local/share/jdk/current" } ],
  "hints": [ { "text": "21.0.5 ist verfügbar", "action": "java.update" } ]
}
```

`status` ist `ok`, `warn`, `error` oder `missing`. Der Kern speichert das Ergebnis für die Dauer aus `cache`, sodass die Übersicht sofort steht.

**Aktionen** laufen im PTY: alles auf `stdout` und `stderr` ist für Menschen und geht ins Terminal. Strukturierte Nachrichten gehen als JSON Lines auf Deskriptor 3:

| `type` | Felder | Bedeutung |
| --- | --- | --- |
| `step` | `id`, `label`, `state` (`running`, `done`, `skipped`, `failed`) | Teilschritt für die Fortschrittsliste |
| `progress` | `percent` (0–100 oder `null`), `text` | Fortschrittsbalken |
| `change` | `kind` (`file`, `package`, `service`, `env`, `link`, `other`), `op` (`create`, `update`, `delete`), `target`, `summary` | eine Änderung; im Probelauf eine geplante |
| `log` | `level` (`info`, `warn`, `error`), `text` | Meldung, die im Verlauf hervorgehoben wird |
| `setting` | `key`, `value` | eigene Einstellung dauerhaft setzen (nur Schlüssel des eigenen Plugins) |
| `notice` | `kind` (`relogin`, `reboot`, `restart-shell`), `text` | Hinweis nach dem Lauf |
| `result` | `summary`, `data` | Abschluss, genau einmal am Ende |

Beispiel:

```json
{"type":"step","id":"download","label":"Temurin 21.0.5 laden","state":"running"}
{"type":"progress","percent":45,"text":"87 von 192 MB"}
{"type":"step","id":"download","label":"Temurin 21.0.5 laden","state":"done"}
{"type":"change","kind":"file","op":"create","target":"~/.local/share/jdk/21.0.5","summary":"JDK entpackt"}
{"type":"change","kind":"env","op":"update","target":"JAVA_HOME","summary":"zeigt auf 21.0.5"}
{"type":"notice","kind":"restart-shell","text":"Neue Terminals nutzen JAVA_HOME sofort, offene erst nach Neustart."}
{"type":"result","summary":"Temurin 21.0.5 installiert und als Standard gesetzt","data":{"version":"21.0.5"}}
```

**Exit-Codes**

| Code | Bedeutung | Zustand der Sitzung |
| --- | --- | --- |
| 0 | erfolgreich, auch wenn nichts zu tun war | `success` |
| 1 | Fehler | `failed` |
| 2 | ungültige Parameter oder Einstellungen | `failed`, Hinweis auf das Feld |
| 3 | Voraussetzung fehlt (Befehl, Netz, Recht) | `blocked` |
| 4 | vom Benutzer im Skript abgelehnt | `stopped` |
| 130, 143 | durch Abbruch beendet | `stopped` |

**Abbruch und Zeitgrenzen.** Abbruch schickt `SIGINT` an die Prozessgruppe, nach 5 Sekunden `SIGTERM`, nach weiteren 5 `SIGKILL`. Bei Überschreitung von `timeout` gleiches Vorgehen, Zustand `timeout`.

**Sperren.** Zwei Aktionen desselben Plugins laufen nie gleichzeitig. Aktionen können zusätzlich benannte Sperren anmelden (`"locks": ["system-packages"]`), etwa alle Schritte, die `apt` oder `dnf` nutzen. Wartende Sitzungen stehen sichtbar als `queued` in der Liste.

**Hilfsbefehle für Shell-Plugins.** Damit Plugins in Bash ohne `jq` auskommen, bietet das Binary `devbox sdk`:

| Befehl | Wirkung |
| --- | --- |
| `devbox sdk param version` | Parameter lesen |
| `devbox sdk setting installDir` | aufgelöste Einstellung lesen |
| `devbox sdk secret` | Geheimnis eines Abnehmers lesen (nur mit Recht) |
| `devbox sdk emit step download running "Laden"` | Nachricht auf Deskriptor 3 schreiben |
| `devbox sdk block ~/.npmrc npm < inhalt` | markierten Block in einer Datei anlegen oder ersetzen, mit Sicherung und `change`-Nachricht, im Probelauf nur melden |
| `devbox sdk block-remove ~/.npmrc npm` | markierten Block entfernen |
| `devbox sdk json-set ~/.config/x.json /pfad/zum/wert '"neu"'` | Wert in einer JSON-Datei setzen, Formatierung bleibt |
| `devbox sdk download URL ZIEL --sha256 …` | Download mit Proxy, CA-Datei, Fortschritt und Prüfsumme |

Für TypeScript gibt es dasselbe als `@zenit-hosting/devbox-sdk`, für Python und Go kleine Bibliotheken im Katalog-Repository.

## Zugangsdaten

Zugangsdaten sind der Teil, der am meisten Zeit kostet und am häufigsten schiefgeht: Tokens laufen ab, stehen in fünf Dateien und sind überall leicht anders eingetragen. DevBox trennt deshalb drei Dinge sauber:

- **Typ** (vom Plugin definiert): welche Felder es gibt, wie geprüft wird, ob ein Ablaufdatum bekannt ist.
- **Instanz** (vom Profil oder Benutzer angelegt): ein konkreter Zugang mit Namen, z. B. `firmen-registry`. Von jedem Typ kann es beliebig viele geben.
- **Abnehmer** (Plugins): schreiben eine Instanz in die Werkzeuge, für die das Profil oder der Benutzer sie eingetragen hat.

**Mitgelieferte Typen**

| Typ | Felder | Geheimnis | Prüfung | Abnehmer |
| --- | --- | --- | --- | --- |
| `registry` | `url`, `layout`, `username` | Token oder Passwort | Anfrage an einen Endpunkt der Registry, je nach `layout` | `npm`, `maven`, `gradle`, `nuget`, `pip`, `docker` |
| `git-host` | `url`, `flavor` (`gitlab`, `github`, `gitea`, `bitbucket`, `generic`), `username` | Personal Access Token | API-Aufruf „aktueller Benutzer“, liest Ablaufdatum und Rechte, wo die API es liefert | `git` (Credential-Helper), `githost` (SSH-Schlüssel hochladen, Token erneuern) |
| `ssh-key` | `path`, `comment` | Passphrase (optional) | Schlüssel lesbar, passt zum öffentlichen Teil | `git`, `githost` |
| `generic` | frei im Profil definiert | beliebig | keine | Plugins, die ihn anmelden |

**Tresor.** `vault.backend` `auto` nimmt den Secret Service (GNOME Keyring, KWallet), wenn eine Sitzung ihn bereitstellt, sonst eine mit `age` verschlüsselte Datei. Der Schlüssel dazu liegt in `~/.local/share/devbox/vault.key` (Modus 0600) oder wird aus einer Passphrase abgeleitet (`vault.passphrase: true`), die der Dienst beim Start abfragt. Ein Wechsel des Backends migriert alle Einträge.

**Ablauf einer Instanz**

1. Anlegen: aus dem Profil (Felder vorbelegt, ggf. gesperrt) oder frei in Oberfläche und CLI.
2. Geheimnis eingeben: in der Oberfläche, mit `devbox cred set firmen-registry` (verdeckte Eingabe) oder per `--from-stdin`.
3. Prüfen: die `verify`-Aktion des Typs läuft; Ergebnis und Ablaufdatum landen in `credentials.json`.
4. Anwenden: alle eingetragenen Abnehmer laufen nacheinander, jeder meldet seine Änderungen. Fehler eines Abnehmers halten die anderen nicht auf.
5. Überwachen: die Prüfung `credentials.expiry` warnt ab `credentials.warnDays` (Standard 14) Tage vor Ablauf, in Übersicht, Kopfzeile und als Desktop-Benachrichtigung.
6. Erneuern: neues Geheimnis eintragen, Schritte 3 und 4 laufen erneut. Wo die API es erlaubt (GitLab: Token-Rotation), bietet das Plugin `githost` eine Aktion, die das Token selbst erneuert.
7. Entfernen: alle Abnehmer laufen mit `remove`, danach wird das Geheimnis gelöscht.

**Wie Abnehmer schreiben.** Wo ein Werkzeug Umgebungsvariablen unterstützt, ist das der bevorzugte Weg, weil dann kein Token in einer Datei steht: DevBox pflegt dazu `~/.config/devbox/env.d/<plugin>.sh` (für Shells) und `~/.config/environment.d/60-devbox.conf` (für systemd-Sitzungen). Die Einstellung `<plugin>.mode` wählt `env` oder `file`. In beiden Fällen gilt: nur markierte Blöcke anfassen, Sicherung vorher, Rechte 0600 für Dateien mit Geheimnissen.

## Eigene Repositories

Neben dem Katalog von zenit-hosting lassen sich beliebige eigene Repositories einbinden: für Plugins, Kataloge, Profile, Projektvorlagen und Systempakete. Alle Arten werden gleich beschrieben, im Schlüssel `repositories` von `config.json` (Benutzer) oder `profile.json` (Team), und gleich verwaltet: über Oberfläche, CLI und API. Ein Profil kann damit einem ganzen Team auf einen Schlag die eigenen Plugins, Vorlagen und Paketquellen mitgeben.

| Art (`kind`) | Inhalt | Wirkung |
| --- | --- | --- |
| `plugins` | ein Ordner mit einem oder mehreren Plugins (je Unterordner eine `plugin.json`) in Git, als Tarball über HTTPS oder lokal | Plugins erscheinen wie mitgelieferte, mit Herkunft; Aktualisieren über `devbox repo sync` |
| `catalog` | ein Katalog mit `index.json`, Versionen und signierten Paketen | Plugins sind suchbar und gezielt installierbar, Versionen in `plugins.lock.json` |
| `profile` | ein Profil in Git, HTTPS oder als Datei | wie ein Eintrag in `profiles` (Kurzform für dieselbe Sache) |
| `templates` | Projektvorlagen, je Unterordner eine Vorlage mit `template.json` | erscheinen im Plugin `templates` beim Anlegen neuer Projekte |
| `packages` | eine Paketquelle des Betriebssystems (`apt`, `rpm`, `pacman`, `zypper`) | das Plugin `base` richtet sie mit Schlüssel ein, Pakete daraus stehen in `base.packages` zur Verfügung |

Paket-Registries für npm, Maven, NuGet, PyPI und Docker sind keine Repositories in diesem Sinn, sondern Zugangs-Instanzen vom Typ `registry` (siehe Zugangsdaten); davon kann es ebenfalls beliebig viele geben, auch mehrere je Werkzeug.

**Beispiel** in `config.json` oder `profile.json`:

```json
{
  "repositories": {
    "team-plugins": {
      "kind": "plugins",
      "source": "git+https://git.example.com/platform/devbox-plugins.git",
      "ref": "main",
      "path": "plugins",
      "trust": "prompt",
      "credential": "firmen-git"
    },
    "firmen-katalog": {
      "kind": "catalog",
      "source": "https://devbox-catalog.example.com/index.json",
      "trust": { "issuer": "https://git.example.com", "identity": "https://git.example.com/platform/devbox-catalog//.ci/release@refs/heads/main" }
    },
    "vorlagen": {
      "kind": "templates",
      "source": "git+https://git.example.com/platform/project-templates.git",
      "ref": "v3"
    },
    "docker-ce": {
      "kind": "packages",
      "format": "apt",
      "url": "https://download.docker.com/linux/${host:distro}",
      "suite": "${host:codename}",
      "components": ["stable"],
      "key": "https://download.docker.com/linux/${host:distro}/gpg",
      "when": { "distro": ["ubuntu", "debian"] }
    },
    "intern-rpm": {
      "kind": "packages",
      "format": "rpm",
      "url": "https://repo.example.com/rpm/$releasever/$basearch/",
      "key": "https://repo.example.com/RPM-GPG-KEY-example",
      "when": { "distro": ["fedora", "rhel"] }
    }
  }
}
```

**Regeln**

- Name des Eintrags passt auf `^[a-z][a-z0-9-]{1,31}$` und ist die ID in CLI, API und Oberfläche. Gleiche Namen in Profil und Benutzerkonfiguration: der Benutzer gewinnt, außer das Profil sperrt den Eintrag mit `"locked": true`.
- `source` kennt `git+https://`, `git+ssh://`, `https://` (Tarball oder `index.json`) und `file://`. `ref` ist ein Branch, Tag oder Commit; Tags oder Commits werden für Teams empfohlen, weil dann jeder denselben Stand hat.
- `credential` verweist auf eine Zugangs-Instanz (`git-host` oder `registry`) für private Repositories. Das Token geht nur an Git oder den Download, nie an Plugins.
- `trust` legt fest, wie Code aus dem Repository freigegeben wird: ein Objekt mit Sigstore-Identität (Signatur Pflicht), `"prompt"` (bei jeder neuen Version die Rechte anzeigen und bestätigen lassen) oder `"pinned"` (nur der in `ref` genannte Commit, ohne Rückfrage). Ohne Angabe gilt `"prompt"`.
- `when` begrenzt einen Eintrag auf Distributionen oder Architekturen, damit ein Profil für gemischte Teams funktioniert.
- Paketquellen schreibt `base` als eigene, markierte Dateien: deb822-Format unter `/etc/apt/sources.list.d/devbox-<name>.sources` mit Schlüssel unter `/etc/apt/keyrings/`, `/etc/yum.repos.d/devbox-<name>.repo`, entsprechend für zypper und pacman. Das braucht `sudo` und läuft als angekündigte Aktion `base.repositories`. Entfernen räumt Datei und Schlüssel wieder weg.
- Plugins aus `plugins`-Repositories reihen sich vor den mitgelieferten in die Suchreihenfolge ein und können sie so gezielt ersetzen; die Oberfläche zeigt das als Hinweis.

**Verwaltung**

| CLI | API | Wirkung |
| --- | --- | --- |
| `devbox repo list` | `GET /repositories` | alle Einträge mit Art, Herkunft (Profil oder Benutzer), Stand, letzter Synchronisation |
| `devbox repo add NAME --kind plugins --source URL [--ref R] [--credential C] [--trust prompt]` | `POST /repositories` | Eintrag anlegen, prüfen, erste Synchronisation |
| `devbox repo sync [NAME]` | `POST /repositories/{name}/sync` | neuen Stand holen, Änderungen und neue Rechte anzeigen |
| `devbox repo remove NAME` | `DELETE /repositories/{name}` | Eintrag entfernen; Plugins daraus werden abgeschaltet, Paketquellen zurückgebaut |

In der Oberfläche gibt es dazu unter Einstellungen den Bereich „Repositories“ mit Liste, Formular aus dem Schema, Synchronisieren und Entfernen. Der Plugin-Katalog `devbox-plugins` von zenit-hosting ist selbst nur ein vorbelegter Eintrag vom Typ `catalog` und kann abgeschaltet oder ersetzt werden, etwa in Netzen ohne Zugang zu GitHub.

Für eigene Plugin-Repositories gibt es eine Vorlage `templates/repository/` mit Ordnerstruktur, CI-Workflow (Validierung, `bats`, optional Signieren) und README, angelegt mit `devbox repo init PFAD`.

## API

Die API ist der einzige Weg, wie Oberfläche, CLI und Terminal-Menü mit dem Kern sprechen. Der Vertrag steht als OpenAPI 3.1 in JSON unter `api/openapi.json`; Go-Handler-Typen und der TypeScript-Client werden daraus erzeugt, Tests prüfen jede Antwort gegen das Dokument. Alle Pfade beginnen mit `/api/v1`, alle Antworten sind JSON mit `camelCase`-Feldern, Zeiten in RFC 3339 UTC.

**Endpunkte**

| Methode und Pfad | Zweck |
| --- | --- |
| `GET /info` | Version, Rechner, Anmeldemodus, Protokollversion, Funktionen |
| `POST /auth/login`, `POST /auth/logout`, `GET /auth/me` | Anmeldung mit Systemkonto (nur bei `server.auth: pam`) |
| `GET /plugins`, `GET /plugins/{id}` | Plugins mit Zustand, Manifest und Grund bei `blocked` oder `invalid` |
| `DELETE /plugins/{id}` | aus Katalog installiertes Plugin entfernen |
| `GET /checks`, `GET /checks/{id}` | zwischengespeicherte Prüfergebnisse |
| `POST /checks/refresh` | Prüfungen neu ausführen, Body `{ "ids": ["java.installed"] }` oder leer für alle |
| `GET /settings` | alle Einstellungen mit Schema, wirksamem Wert, Herkunft und Sperre |
| `PATCH /settings` | Benutzerwerte setzen, `null` entfernt den Benutzerwert: `{ "node.channel": "current" }` |
| `GET /settings/{key}/explain` | alle Ebenen eines Schlüssels |
| `GET /profiles`, `POST /profiles`, `DELETE /profiles/{id}`, `POST /profiles/update` | Profile verwalten |
| `GET /credentials`, `POST /credentials`, `DELETE /credentials/{name}` | Instanzen ohne Geheimnis |
| `PUT /credentials/{name}/secret` | Geheimnis setzen, Body `{ "secret": "…", "expiresAt": null }`, startet Prüfung und Anwenden als Sitzung |
| `POST /credentials/{name}/verify`, `POST /credentials/{name}/apply` | einzeln auslösen, Antwort ist eine Sitzung |
| `GET /sessions` | Liste mit Filter `state`, `plugin`, `limit`, `cursor` |
| `POST /sessions` | Aktion starten: `{ "action": "java.install", "params": { "version": 21 }, "dryRun": false, "cols": 120, "rows": 32 }` |
| `GET /sessions/{id}`, `GET /sessions/{id}/events`, `GET /sessions/{id}/log` | Zustand, Nachrichten, Rohausgabe (mit `Range`) |
| `POST /sessions/{id}/cancel` | abbrechen |
| `GET /groups`, `POST /groups/{id}/run` | Gruppen ansehen und als Folge von Sitzungen starten |
| `GET /setup`, `POST /setup/run` | Plan der Ersteinrichtung mit Zustand je Schritt, Ausführen ab dem ersten offenen Schritt |
| `GET /catalog/search?q=`, `POST /catalog/install` | Plugins im Katalog finden und installieren |
| `GET /schema/{name}` | eingebettete JSON-Schemas für Editoren und Formulare |

**WebSocket** `GET /api/v1/ws`: eine Verbindung je Fenster, Nachrichten als JSON-Objekte mit `type`. Terminal-Daten sind Base64, damit Bytes unverändert ankommen.

| Richtung | `type` | Felder |
| --- | --- | --- |
| Client → Kern | `subscribe` | `topics`: `checks`, `sessions`, `settings`, `credentials`, `session:<id>`; bei Sitzungen optional `fromSeq` für Wiederaufnahme |
| Client → Kern | `unsubscribe` | `topics` |
| Client → Kern | `input` | `session`, `data` (Base64), z. B. für das `sudo`-Passwort |
| Client → Kern | `resize` | `session`, `cols`, `rows` |
| Kern → Client | `output` | `session`, `seq`, `data` (Base64) |
| Kern → Client | `event` | `session`, `seq`, `event` (eine Protokoll-Nachricht wie oben) |
| Kern → Client | `session` | `session` (Zusammenfassung: Zustand, Titel, Zeiten, Fortschritt) |
| Kern → Client | `check` | `id`, `result` |
| Kern → Client | `settings` | `keys` (geänderte Schlüssel) |
| Kern → Client | `credential` | `name`, `state`, `expiresAt` |
| beide | `ping`, `pong` | alle 30 Sekunden |

Der Kern hält je Sitzung die letzten 2 MiB Ausgabe im Speicher; mit `fromSeq` holt sich ein neu verbundenes Fenster den Rest, ältere Ausgabe kommt aus `GET /sessions/{id}/log`.

**Fehlerformat** nach RFC 9457 (`application/problem+json`) mit stabilem `code`:

```json
{
  "type": "https://devbox.zenit-hosting.de/errors/setting-locked",
  "title": "Einstellung gesperrt",
  "status": 409,
  "code": "setting-locked",
  "detail": "Der Wert von java.vendorApi ist im Profil Beispiel-Team gesperrt.",
  "instance": "/api/v1/settings",
  "errors": [ { "pointer": "/java.vendorApi", "code": "locked", "message": "gesperrt durch profile:example-team" } ]
}
```

| `code` | Status | Wann |
| --- | --- | --- |
| `validation` | 422 | Body oder Parameter passt nicht zum Schema, Details in `errors` |
| `not-found` | 404 | Plugin, Aktion, Sitzung oder Instanz unbekannt |
| `plugin-not-ready` | 409 | Plugin ist `blocked`, `invalid`, `disabled` oder `incomplete` |
| `setting-locked` | 409 | gesperrter Wert soll geändert werden |
| `permission-denied` | 403 | Plugin hat ein Recht nicht angemeldet |
| `unauthenticated` | 401 | keine gültige Anmeldung |
| `csrf` | 403 | Kopfzeile `X-DevBox-Request` oder Origin fehlt oder passt nicht |
| `conflict` | 409 | gleiche Aktion läuft schon und ist nicht wiederholbar |
| `internal` | 500 | Fehler im Kern, mit `traceId` für das Log |

**Schutz.** Verändernde Anfragen brauchen die Kopfzeile `X-DevBox-Request: 1` und einen `Origin`, der zur Adresse des Dienstes oder zu `server.allowedOrigins` passt. Das Sitzungs-Cookie ist `HttpOnly`, `SameSite=Strict` und bei TLS `Secure`. Die CLI spricht über den Unix-Socket `$XDG_RUNTIME_DIR/devbox/api.sock` (Modus 0600) und braucht keine Anmeldung.

## CLI

Die CLI ist vollständig: alles, was die Oberfläche kann, geht auch hier. Ohne Argumente in einem Terminal startet `devbox` das Terminal-Menü. Jeder lesende Befehl kennt `--json` und gibt dann genau ein JSON-Dokument aus, das dem Schema der passenden API-Antwort entspricht; `devbox run --json` gibt die Sitzungsereignisse als JSON Lines aus.

| Befehl | Zweck |
| --- | --- |
| `devbox` | Terminal-Menü |
| `devbox serve [--listen ADR] [--open]` | Dienst im Vordergrund starten |
| `devbox service install\|start\|stop\|status\|uninstall` | systemd-Benutzerdienst verwalten |
| `devbox status` | Kurzübersicht aller Prüfungen |
| `devbox check [ID …] [--refresh]` | Prüfungen anzeigen oder neu ausführen |
| `devbox actions [PLUGIN]` | verfügbare Aktionen mit Parametern |
| `devbox run AKTION [-p name=wert …] [--dry-run] [--yes]` | Aktion ausführen, Ausgabe live |
| `devbox group run ID` | Gruppe ausführen, z. B. `update-all` |
| `devbox setup [--profile QUELLE] [--dry-run] [--yes]` | Ersteinrichtung nach Plan, fortsetzbar |
| `devbox config get\|set\|unset\|explain\|edit\|validate` | Einstellungen lesen, setzen, Herkunft zeigen, Datei im Editor öffnen und prüfen |
| `devbox profile add\|list\|show\|update\|remove` | Profile verwalten |
| `devbox cred list\|add\|set\|verify\|apply\|remove` | Zugangsdaten verwalten, `set` liest verdeckt oder mit `--from-stdin` |
| `devbox plugin list\|info\|install\|update\|remove` | Plugins und Katalog |
| `devbox plugin new NAME [--lang bash\|ts\|python\|go]` | neues Plugin aus Vorlage anlegen |
| `devbox plugin validate PFAD` | Manifest, Skripte und Rechte prüfen |
| `devbox plugin test PFAD` | Prüfungen und Probeläufe aller Aktionen in einer Sandbox ausführen |
| `devbox history [--limit N]`, `devbox logs SITZUNG` | Verlauf und Ausgabe einer Sitzung |
| `devbox doctor` | Selbsttest: Dienst, Socket, Tresor, Schemas, Plugins, Netz, Proxy |
| `devbox schema print NAME` | eingebettetes Schema ausgeben |
| `devbox migrate --from-v2` | Einstellungen aus DevBox 2 übernehmen |
| `devbox version` | Version, Commit, Build-Datum, Protokoll |
| `devbox completion bash\|zsh\|fish` | Vervollständigung |
| `devbox sdk …` | Hilfsbefehle für Plugins (siehe Protokoll) |

**Globale Schalter:** `--json`, `--quiet`, `--no-color` (auch `NO_COLOR`), `--config PFAD`, `--set schlüssel=wert`, `--lang de|en`, `--socket PFAD`.

**Exit-Codes** wie im Plugin-Protokoll, dazu `5` für `devbox status --strict`, wenn mindestens eine Prüfung `warn` meldet, und `6`, wenn eine `error` oder `missing` meldet. So lässt sich DevBox in Skripten und CI nutzen.

Beispiele:

```json
$ devbox status --json
{
  "host": { "name": "dev-vm-07", "user": "m.muster", "distro": "ubuntu", "distroVersion": "24.04" },
  "summary": { "ok": 14, "warn": 2, "error": 0, "missing": 1 },
  "checks": [
    { "id": "java.installed", "status": "warn", "summary": "Temurin 21.0.4 (Standard), 17.0.12", "checkedAt": "2026-10-27T09:18:02Z" },
    { "id": "credentials.expiry", "status": "warn", "summary": "firmen-git läuft in 9 Tagen ab", "checkedAt": "2026-10-27T09:18:02Z" }
  ]
}
```

```json
$ devbox config explain java.vendorApi --json
{
  "key": "java.vendorApi",
  "value": "https://mirror.example.com/adoptium/v3",
  "origin": "profile:example-team",
  "locked": true,
  "layers": [
    { "layer": "plugin", "value": "https://api.adoptium.net/v3" },
    { "layer": "profile:example-team", "value": "https://mirror.example.com/adoptium/v3", "locked": true }
  ]
}
```

## Oberfläche

Die Oberfläche enthält keinen Code für einzelne Plugins. Sie baut jede Seite aus Manifesten, Prüfergebnissen und Schemas. Ein neues Plugin erscheint deshalb sofort mit Karte, Aktionen, Formularen und Einstellungen, ohne dass die Oberfläche neu gebaut wird.

**Seiten**

| Route | Inhalt | Datenquelle |
| --- | --- | --- |
| `/` Übersicht | Karten je Plugin nach Kategorie, Zustand, Hinweise mit Direktaktion, Profil-Hinweis, „Alles aktualisieren“ | `GET /checks`, `GET /plugins`, `contributes.overview` |
| `/einrichtung` | Plan der Ersteinrichtung als Schrittliste mit Zustand, Start ab dem ersten offenen Schritt | `GET /setup` |
| `/plugins/:id` | Detailseite: Prüfungen mit Details, Aktionen mit Parameterformular, Einstellungen des Plugins | Manifest, Prüfungen |
| `/zugaenge` | Instanzen mit Zustand und Ablauf, Anlegen, Token setzen, Prüfen, Anwenden, Entfernen | `GET /credentials` |
| `/zugaenge/:name` | Formular aus Feldern des Typs, Abnehmer mit Optionen, letzter Anwendungslauf | Typ-Schema, Instanz |
| `/einstellungen` | alle Einstellungen nach Plugin gruppiert, Herkunft und Sperre je Feld, Suche, Profile, Erscheinungsbild | `GET /settings`, `GET /profiles` |
| `/plugins` | installierte Plugins mit Zustand, Katalog-Suche, Installieren und Entfernen | `GET /plugins`, `GET /catalog/search` |
| `/verlauf` | Sitzungen mit Filter, Ausgabe, Änderungen, Sicherungen | `GET /sessions` |
| Terminal-Dock | an jeder Seite unten, mehrere Sitzungen als Reiter, Eingabe für `sudo` | WebSocket |

**Formulare aus Schemas.** Parameter, Einstellungen und Zugangsfelder sind JSON-Schema-Ausschnitte. Eine Komponente `SchemaForm` setzt sie auf zenit-ui-Felder um:

| Schema | Feld |
| --- | --- |
| `string` | Textfeld; `format: uri` mit Prüfung, `format: path` mit Platzhalter-Vorschau |
| `string` mit `enum` | Auswahl (bis 5 Werte als Segment, sonst Liste) |
| `integer`, `number` | Zahlenfeld mit `minimum`, `maximum` |
| `boolean` | Schalter |
| `array` von `string` | Liste mit Hinzufügen und Entfernen, `pattern` je Eintrag |
| `optionsFrom` | Auswahl, gefüllt aus Prüfdaten |
| `secret` | Passwortfeld, nie vorbelegt, Anzeige nur „gesetzt am …“ |
| `advanced: true` | unter „Weitere Optionen“ eingeklappt |

Fehler aus `validation`-Antworten werden über `pointer` dem Feld zugeordnet. Gesperrte Werte sind schreibgeschützt und tragen den Namen des Profils.

**Zustand und Daten.** Ein `Store` je Bereich auf Basis von Signals, gefüttert aus einer einzigen WebSocket-Verbindung. Nach einem Neuverbinden holt der Store den vollen Stand per REST und abonniert neu. Keine Seite fragt in Schleifen ab.

**Sprachen als Plugins.** Der Kern enthält keine Texte. Jede Sprache ist ein Sprachpaket, ein gewöhnliches Plugin mit dem Beitrag `contributes.language`. Mitgeliefert und vorinstalliert sind `lang-de` (Deutsch) und `lang-en` (Englisch); weitere wie `lang-fr` oder `lang-nl` kommen aus Katalogen oder eigenen Repositories und werden wie jedes andere Plugin installiert.

Ein Sprachpaket liefert:

- `messages/core.json`: alle Texte von Oberfläche, CLI, Terminal-Menü und Fehlermeldungen (flaches JSON, Schlüssel in Punktschreibweise, ICU-Platzhalter),
- `messages/plugins/<id>.json`: Übersetzungen für Beschriftungen, Hilfetexte und Rückfragen anderer Plugins, damit auch Plugins ohne eigene Übersetzung in der Sprache erscheinen,
- Formate für Datum, Zahlen und Größenangaben über den Sprachcode (Intl in der Oberfläche, `golang.org/x/text` im Kern).

```json
{
  "$schema": "https://devbox.zenit-hosting.de/schema/plugin.v1.json",
  "id": "lang-fr",
  "version": "1.0.0",
  "name": "Français",
  "description": "Langue française pour DevBox",
  "category": "language",
  "license": "Apache-2.0",
  "requires": { "devbox": ">=1.0.0" },
  "contributes": {
    "language": {
      "code": "fr",
      "name": "Français",
      "englishName": "French",
      "fallback": "en",
      "messages": "messages"
    }
  }
}
```

**Reihenfolge beim Nachschlagen** eines Textes: eigene Übersetzung des Plugins (`i18n/<code>.json`) → Sprachpaket (`messages/plugins/<id>.json` bzw. `core.json`) → Ersatzsprache aus `fallback` → Englisch → Text direkt im Manifest. Fehlende Schlüssel werden nie als leerer Text gezeigt. `devbox plugin validate` meldet die Abdeckung eines Sprachpakets in Prozent, die Sprachauswahl zeigt sie neben dem Namen.

**Auswahl.** Die Ersteinrichtung beginnt mit der Sprache, noch vor allen anderen Schritten. Die Liste zeigt alle installierten Sprachpakete in ihrer eigenen Sprache („Deutsch“, „English“, „Français“), vorausgewählt ist die Sprache aus `$LANG`, falls ein Paket dafür da ist, sonst die Vorgabe des Profils (`"ui": { "language": "de" }`), sonst Englisch. Bringt ein Profil weitere Sprachpakete über `plugins.require` mit, erscheinen sie schon in dieser Auswahl. Später lässt sich die Sprache unter Einstellungen → Erscheinungsbild, mit `devbox lang list|set CODE` oder im Terminal-Menü wechseln; der Wert steht in `ui.language` und gilt für Oberfläche, CLI und Terminal-Menü gleichermaßen, `--lang` überschreibt ihn für einen Aufruf. `lang-de` und `lang-en` lassen sich nicht entfernen, nur aktualisieren, damit immer eine vollständige Sprache vorhanden ist. Alle Texte sind neutral formuliert.

**Anpassbar über Profil.** Titel in Kopfzeile und Browser-Tab, Akzentfarbe aus den zenit-ui-Akzenten, Links in der Kopfzeile, ein Hinweistext auf der Übersicht. Logo als SVG-Pfad im Profil (maximal 32 KiB, wird bereinigt).

**Barrierefreiheit.** Ziel WCAG 2.2 AA: vollständige Tastaturbedienung, sichtbarer Fokus, Sprunglink, Live-Region für Sitzungsmeldungen, Kontrast-Theme. Playwright-Tests laufen mit `axe-core`.

**Terminal-Menü.** Bubble-Tea-Oberfläche mit denselben Bereichen (Übersicht, Einrichtung, Plugins, Zugänge, Einstellungen, Verlauf) und denselben Formularen aus Schemas, gerendert als Textfelder und Auswahllisten. Gedacht für SSH-Sitzungen und Rechner ohne Browser.

## Mitgelieferte Plugins

Version 1.0 bringt 20 Plugins im Repository unter `plugins/` mit. Sie decken ab, was DevBox 2 konnte, und sind zugleich die Referenz für Plugin-Autoren. Jede Adresse in der Spalte „Quellen“ ist nur ein Standardwert und über eine Einstellung änderbar, etwa auf einen Spiegel im eigenen Netz.

| Plugin | Kategorie | Kann | Wichtige Einstellungen | Quellen (Standard) |
| --- | --- | --- | --- | --- |
| `system` | Kern | Rechnerdaten, Umgebungs-Lader für `env.d` in Shell und systemd, Selbst-Update von DevBox | `updateChannel`, `releaseUrl` | GitHub-Releases von zenit-hosting/devbox |
| `base` | System | Grundpakete über apt, dnf oder pacman, Systemupdate | `packages`, `extraPackages`, `upgradeMode` | Paketquellen der Distribution |
| `node` | Laufzeit | Node.js installieren, wechseln, aktualisieren, globale Pakete mitnehmen | `channel`, `distUrl`, `manager` (`direct`, `nvm`), `globalPackages` | nodejs.org/dist |
| `java` | Laufzeit | JDKs installieren, Standard setzen, alte Patchstände entfernen | `vendorApi`, `defaultVersion`, `installDir`, `keepPatches` | api.adoptium.net |
| `dotnet` | Laufzeit | .NET-SDKs nach Kanal oder Version, Workloads | `channel`, `metadataUrl`, `installScriptUrl`, `installDir` | Release-Metadaten von Microsoft, dot.net/v1/dotnet-install.sh |
| `python` | Laufzeit | uv installieren, Python-Versionen, Standardversion, Werkzeuge über `uv tool` | `uvInstallUrl`, `versions`, `defaultVersion`, `tools` | astral.sh, python-build-standalone |
| `docker` | Werkzeug | Docker Engine, Gruppe, `daemon.json` (Spiegel, Logs, Netze), Abnehmer für `registry` (`docker login`) | `source` (`distro`, `docker`), `registryMirrors`, `rootless` | Paketquellen von Distribution oder Docker |
| `git` | Werkzeug | Identität, Standardwerte, Credential-Helper, Abnehmer für `git-host` | `defaults` (Objekt mit git-config-Werten), `credentialHelper` | keine |
| `githost` | Werkzeug | SSH-Schlüssel erzeugen und hochladen, Token prüfen und erneuern für GitLab, GitHub, Gitea | `keyType`, `tokenScopes` | API der jeweiligen Instanz |
| `registry` | Zugänge | Zugangstyp `registry`, Prüfung je Layout | `verifyPaths` je Layout | Adresse der Instanz |
| `npm` | Abnehmer | `.npmrc` oder Umgebung für Registry und Scopes | `mode`, `alwaysAuth` | registry.npmjs.org als Rückfall |
| `maven` | Abnehmer | `settings.xml`: Server, Spiegel, Profile für Releases und Snapshots | `mode`, `settingsPath` | Maven Central als Rückfall |
| `gradle` | Abnehmer | `gradle.properties` oder `ORG_GRADLE_PROJECT_*` | `mode`, `propertyPrefix` | keine |
| `nuget` | Abnehmer | `NuGet.Config` oder `NuGetPackageSourceCredentials_*` | `mode`, `sourceName` | nuget.org als Rückfall |
| `pip` | Abnehmer | `pip.conf`, `uv.toml`, `.netrc` oder `PIP_INDEX_URL`, `UV_DEFAULT_INDEX` | `mode`, `targets` (`pip`, `uv`, `poetry`) | pypi.org als Rückfall |
| `templates` | Projekte | neue Projekte aus Vorlagen anlegen (Node, Java, .NET, Python und eigene) | `sources` (Liste von Git-Adressen mit Vorlagen), `projectRoot` | Ordner templates/ in devbox-plugins |
| `xrdp` | Remote | xrdp mit XFCE: Installation, Port, Farbtiefe, Sicherheit, Ton, Schlüsselbund, polkit, Desktop-Feinschliff, Status, Neustart, Abschalten | `port`, `securityLayer`, `maxBpp`, `sound`, `desktop` | Paketquellen der Distribution |
| `cleanup` | Wartung | Caches (npm, Maven, Gradle, NuGet, pip, uv, Docker) und Build-Ordner in Projekten aufräumen, mit Größen im Probelauf | `projectRoots`, `olderThanDays`, `patterns`, `docker` | keine |
| `lang-de` | Sprache | alle Texte auf Deutsch, vorinstalliert, nicht entfernbar | keine | keine |
| `lang-en` | Sprache | alle Texte auf Englisch, vorinstalliert, nicht entfernbar, letzte Ersatzsprache | keine | keine |

Die Gruppe `update-all` bekommt Beiträge von `base`, `node`, `java`, `dotnet`, `python`, `docker` und `system`; `cleanup-all` von `cleanup`. Ein Profil kann beide Gruppen neu zusammenstellen.

**Ausgelagert in den Katalog** (`zenit-hosting/devbox-plugins`), weil nicht jeder sie braucht: `kubectl`, `helm`, `terraform`, `go`, `rust`, `vscode`, `jetbrains-toolbox`, `podman`, `sdkman`. Sie zeigen zugleich, wie ein Plugin außerhalb des Kern-Repositorys entsteht.

## Sicherheit

DevBox führt Skripte mit den Rechten des Benutzers aus, verwaltet Tokens und kann `sudo` anstoßen. Das Sicherheitsmodell geht davon aus, dass mitgelieferte Plugins und Plugins aus vertrauenswürdigen Katalogen gutartig sind, aber Fehler haben können, und dass die Weboberfläche aus dem Netz erreichbar sein kann.

| Risiko | Maßnahme |
| --- | --- |
| Fremder Code aus Katalogen | Installation nur aus Einträgen in `repositories`. Bei Katalogen ist die Sigstore-Signatur mit erwarteter Identität Pflicht, bei Plugin-Repositories gilt `trust` (`prompt`, `pinned` oder Signatur). Digest in `plugins.lock.json`. Vor der Installation zeigt DevBox die angemeldeten Rechte zur Bestätigung; bei einem Update mit neuen Rechten erneut. |
| Plugins tun mehr als angemeldet | Ausführung über `systemd-run --user --scope` mit Schreibrechten nur auf angemeldete Pfade (`ReadWritePaths`, `ProtectHome=read-only`); ohne systemd über `bubblewrap`, falls vorhanden. Netzziele werden in 1.0 angezeigt und protokolliert, ab 1.1 über einen lokalen Proxy erzwungen. Lokale Plugins ohne Signatur laufen nur nach ausdrücklicher Freigabe (`devbox plugin trust PFAD`). |
| Geheimnisse gelangen in Logs | Geheimnisse nur über die Anfrage-Datei; der Runner ersetzt bekannte Geheimnisse in Ausgabe und Ereignissen durch `••••`, bevor etwas gespeichert oder gesendet wird. |
| Geheimnisse in Dateien | Bevorzugt Umgebung statt Datei; Dateien mit Geheimnissen immer 0600; `devbox doctor` meldet zu offene Rechte. |
| Zugriff auf die Oberfläche aus dem Netz | Standard ist `127.0.0.1` ohne Login. Andere Adressen nur mit `server.auth: pam` und TLS; ohne Zertifikat erzeugt DevBox ein selbstsigniertes und zeigt den Fingerabdruck. Ratenbegrenzung für Login (5 Versuche je Minute und Adresse). |
| CSRF, DNS-Rebinding | Pflicht-Kopfzeile, Prüfung von `Origin` und `Host`, `SameSite=Strict`, Content-Security-Policy ohne `unsafe-inline` für Skripte. |
| `sudo` | Kein Dienst mit Root. Aktionen mit `"sudo": true` sind gekennzeichnet, laufen nur im PTY mit Passwortabfrage und nie unbeaufsichtigt (`--yes` beantwortet keine `sudo`-Abfrage). Die xrdp-polkit-Regel ist eng auf die nötigen Aktionen begrenzt. |
| Manipulierte Profile | HTTPS oder Git mit festem Ref; optional Signaturpflicht je Profilquelle. Profile können keine Skripte ausführen, nur Werte setzen und Plugins aus Katalogen verlangen. |
| Lieferkette des Projekts | Abhängigkeiten mit Renovate, `govulncheck` und `npm audit` in CI, SBOM (CycloneDX in JSON) und SLSA-Provenance für jedes Release, Actions mit Commit-SHA gepinnt, OpenSSF Scorecard. |

Sicherheitslücken werden über GitHub Security Advisories gemeldet (`SECURITY.md`), mit Antwort innerhalb von 5 Werktagen.

## Repository, Build und Releases

Ein Repository für Kern, Oberfläche, mitgelieferte Plugins, SDK und Doku. So bleiben Schema, API, Oberfläche und Plugins in einem Commit konsistent.

**Zunächst zwei Repositories:**

| Repository | Inhalt |
| --- | --- |
| `zenit-hosting/devbox` | Kern, Oberfläche, Schemas, API-Vertrag, SDK, mitgelieferte Plugins inklusive `lang-de` und `lang-en`, Doku-Quellen; GitHub Pages für Doku-Webseite und Paketquellen |
| `zenit-hosting/devbox-plugins` | Katalog mit `index.json`, Plugins außerhalb des Kerns, weitere Sprachpakete unter `languages/`, Projektvorlagen unter `templates/`, Beispiel-Profil unter `profiles/example/` |

Die Oberfläche bleibt bewusst im Kern-Repository: Sie ist per `go:embed` Teil des Binarys, ihre Typen werden aus denselben Schemas erzeugt, und Ende-zu-Ende-Tests prüfen beide zusammen. Getrennt wird innerhalb des Repositorys (`web/` als eigener npm-Workspace mit eigenen Lint- und Test-Zielen, Zugriff nur über die API, eigene CODEOWNERS), sodass ein späteres Auslagern ohne Umbau ginge.

Später auslagerbar, sobald es einen eigenen Takt oder eigene Beitragende gibt: Sprachpakete (`devbox-languages`, etwa für Weblate), Vorlagen (`devbox-templates`), Beispiel-Profil als GitHub-Template-Repository (`devbox-profile-template`), Doku-Webseite (`devbox-website`) und Paketquellen (`devbox-packages`). Weil DevBox alle diese Quellen über `repositories` einbindet, ändert ein Umzug nur Adressen in der Standardkonfiguration, keinen Code.

```text
devbox/
├── CLAUDE.md                  Arbeitsregeln für Claude Code (siehe unten)
├── Makefile                   einziger Einstieg für Build, Test, Lint, Generate
├── go.mod
├── package.json               npm-Workspaces: web, sdk/ts
├── cmd/devbox/main.go
├── internal/
│   ├── api/                   HTTP, WebSocket, Auth, statische Dateien
│   ├── catalog/               Kataloge, Installation, Signaturen
│   ├── cli/                   cobra-Befehle
│   ├── config/                Ebenen, Platzhalter, Herkunft
│   ├── history/               Verlauf, Sicherungen
│   ├── platform/              Distribution, Paketmanager, systemd, XDG
│   ├── plugin/                Manifeste, Zustände, Ereignisse
│   ├── profile/               Quellen, Cache, Signaturen
│   ├── runner/                PTY, Sandbox, Sitzungen, Sperren
│   ├── schema/                eingebettete Schemas, Validierung
│   ├── sdk/                   devbox sdk …
│   ├── tui/                   Terminal-Menü
│   ├── vault/                 Schlüsselbund, age
│   └── gen/                   erzeugte Typen (nie von Hand ändern)
├── schema/                    *.v1.json – Quelle der Wahrheit
├── api/openapi.json           API-Vertrag
├── plugins/<id>/              plugin.json, checks/, actions/, consume/, i18n/, tests/
├── web/                       Angular-App (src/app/core, shell, pages, shared, schema-form; src/i18n)
├── sdk/ts/                    @zenit-hosting/devbox-sdk
├── templates/plugin/          Vorlagen für devbox plugin new (bash, ts, python, go)
├── packaging/                 systemd-Unit, install.sh, Vervollständigungen, AUR
├── test/                      testscript/, e2e/ (Playwright), fixtures/, images/ (Test-Container je Distribution)
├── docs/                      Benutzer- und Entwicklerdoku (Markdown), adr/
└── .github/                   Workflows, Vorlagen, CODEOWNERS
```

**Make-Ziele**

| Ziel | Wirkung |
| --- | --- |
| `make generate` | Go- und TypeScript-Typen aus `schema/` und `api/openapi.json` erzeugen; CI prüft, dass danach kein Diff bleibt |
| `make build` | Oberfläche bauen, nach `internal/api/dist` kopieren, Binary mit `go:embed` bauen |
| `make dev` | Kern mit `-tags dev` (liefert die Oberfläche vom Angular-Dev-Server aus) und `ng serve` parallel |
| `make test` | Go-Tests mit `-race`, testscript, Vitest, `bats` für alle Plugins |
| `make e2e` | Playwright gegen ein frisch gebautes Binary in einem Test-Container |
| `make lint` | `golangci-lint`, `shellcheck`, ESLint, Prettier-Prüfung, `devbox plugin validate` für alle Plugins, Schema-Prüfung aller JSON-Dateien |
| `make snapshot` | GoReleaser im Snapshot-Modus, alle Pakete lokal |

**JSON auch für die Werkzeuge.** `tsconfig.json`, `angular.json`, `package.json`, `.golangci.json`, `.prettierrc.json`, `renovate.json`, `release-please-config.json`, `.release-please-manifest.json`, `lefthook.json`, `.commitlintrc.json`. Ausnahmen gibt es nur, wo ein Werkzeug nichts anderes kann: GitHub-Workflows (`.github/workflows/*.yml`), `.goreleaser.yaml`, `eslint.config.mjs`, `go.mod`, `Makefile`. Die Ausnahmen stehen in `CONTRIBUTING.md`, damit niemand weitere einführt.

**Releases.** Conventional Commits, release-please öffnet den Release-PR mit `CHANGELOG.md`. Nach dem Merge baut GoReleaser für `linux/amd64` und `linux/arm64`: Tarball, `.deb`, `.rpm`, AUR-Paket, Prüfsummen, cosign-Signaturen, SBOM (CycloneDX JSON) und SLSA-Provenance. Die Paketquellen für apt und dnf liegen auf GitHub Pages unter `pkg.devbox.zenit-hosting.de`, signiert mit einem GPG-Schlüssel des Projekts (apt und dnf verlangen das).

**Installation**

```text
curl -fsSL https://devbox.zenit-hosting.de/install.sh | sh
curl -fsSL https://devbox.zenit-hosting.de/install.sh | sh -s -- --profile https://git.example.com/platform/devbox-profile.git
```

Das Skript erkennt Architektur und Distribution, lädt das Release, prüft Prüfsumme und (wenn `cosign` vorhanden) Signatur, installiert nach `~/.local/bin` (ohne Root) oder mit `--system` nach `/usr/local/bin`, prüft den Namenskonflikt, richtet den Benutzerdienst ein und öffnet die Ersteinrichtung. Schalter: `--version`, `--profile`, `--no-service`, `--bin-name`, `--system`.

## Qualität

**Tests nach Ebene**

| Ebene | Werkzeug | Was geprüft wird | Ziel |
| --- | --- | --- | --- |
| Schemas | `go test` in `internal/schema` | jedes Beispiel aus `docs/` und `test/fixtures/valid/` ist gültig, jedes aus `test/fixtures/invalid/` ungültig mit erwartetem Fehlerpfad | 100 % der Schemas |
| Go-Einheiten | `go test -race` | Konfigurations-Ebenen, Platzhalter, Zustandsmaschine der Sitzungen, Protokoll-Parser, Tresor | 80 % Abdeckung in `internal/` |
| CLI-Abläufe | `testscript` (`.txtar`) | Befehle mit erwarteter Ausgabe und `--json`, Exit-Codes, mit Fake-Plugins | jeder Befehl |
| API | Go-Tests gegen `httptest` | jede Antwort gegen `api/openapi.json` validiert, Fehlerformat, CSRF-Regeln | jeder Endpunkt |
| Plugins | `bats` mit `devbox plugin test` | Prüfungen liefern gültiges JSON, Probelauf ändert nichts, zweiter Lauf meldet keine Änderung | jedes Plugin, jede Aktion |
| Oberfläche | Vitest | Stores, `SchemaForm` für alle Schema-Typen, Pipes | 70 % Abdeckung |
| Ende zu Ende | Playwright mit `axe-core` | Ersteinrichtung, Token setzen und anwenden, Aktion mit Terminal, Abbruch, Theme, Tastatur | Hauptabläufe |
| Distributionen | Container-Matrix (Ubuntu 24.04, Debian 13, Fedora aktuell, Arch) | `install.sh`, `devbox setup --yes` mit Testprofil, Idempotenz | jede Release |

**CI** (GitHub Actions, alle Actions per Commit-SHA gepinnt): `lint` → `generate` ohne Diff → `test` → `build` → `e2e` → Distributions-Matrix (nur auf `main` und Release-PRs). Pull Requests brauchen grüne Checks und ein Review von CODEOWNERS.

**Konventionen**

- Go: `gofumpt`, `golangci-lint` mit `errcheck`, `revive`, `gosec`, `staticcheck`; Fehler mit `%w` einpacken; kein `panic` außerhalb von `main`; Kontexte bis in jeden Prozessstart.
- TypeScript: `strict`, keine `any`, Standalone-Komponenten, `OnPush`, Signals statt RxJS außer für WebSocket-Ströme.
- Shell: `#!/usr/bin/env bash`, `set -euo pipefail`, `shellcheck` ohne Ausnahmen, keine Abhängigkeit außer `devbox sdk` und den in `requires.commands` angemeldeten Befehlen.
- JSON: zwei Leerzeichen, abschließender Zeilenumbruch, jede Datei mit `$schema`.
- Commits: Conventional Commits mit DCO-Zeile (`Signed-off-by`), geprüft durch commitlint und den DCO-Check.
- Texte: neutral formuliert, deutsch und englisch gleichzeitig gepflegt.

**Definition of Done** für jedes Arbeitspaket:

- [ ] Schema und OpenAPI angepasst, `make generate` ohne Diff
- [ ] Tests auf allen betroffenen Ebenen, `make test` und `make lint` grün
- [ ] Probelauf und Idempotenz für jede neue Aktion getestet
- [ ] Texte in `de.json` und `en.json`
- [ ] Doku unter `docs/` aktualisiert, Beispiele validiert
- [ ] Keine fest eingebaute Adresse, kein fest eingebauter Pfad (Prüfung per `make lint`: Regel gegen URL-Literale außerhalb von `default`-Feldern)
- [ ] CHANGELOG-taugliche Commit-Nachricht

## Open Source

| Thema | Entscheidung | Grund |
| --- | --- | --- |
| Lizenz | Apache-2.0 für Kern, Plugins und SDK | erlaubt Nutzung in Unternehmen, enthält Patentklausel, mit zenit-ui (MIT) vereinbar |
| Beiträge | DCO (`git commit -s`), kein CLA | geringe Hürde, rechtlich ausreichend |
| Verhaltenskodex | Contributor Covenant 2.1 | verbreiteter Standard |
| Versionen | SemVer für Kern; Schemas und Protokoll mit eigener Hauptversion (`v1`) | Plugins und Profile bleiben über Kern-Updates stabil |
| Kompatibilität | innerhalb einer Hauptversion nur Ergänzungen; Abkündigungen mindestens eine Minor-Version vorher mit Warnung | Teams können sich auf Profile verlassen |
| Governance | Maintainer-Modell: Entscheidungen per Lazy Consensus im Issue, bei Streit Mehrheit der Maintainer; neue Maintainer nach nachhaltigen Beiträgen auf Vorschlag | einfach, üblich, nachvollziehbar |
| Rechteinhaber | Zenit-Hosting, Hinweis `Copyright 2026 Zenit-Hosting and DevBox contributors` in `LICENSE` und `NOTICE` | klare Herkunft, Beitragende bleiben Urheber ihrer Änderungen |
| Kommunikation | GitHub Issues und Discussions, Roadmap als GitHub Project | alles öffentlich an einem Ort |
| Marke | Name und Logo gehören Zenit-Hosting; Forks dürfen den Code nutzen, aber nicht unter dem Namen DevBox von Zenit-Hosting auftreten | schützt Nutzer vor Verwechslung |

Pflichtdateien im Repository: `README.md`, `LICENSE`, `NOTICE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `GOVERNANCE.md`, `MAINTAINERS.md`, `CHANGELOG.md`, `CLAUDE.md`, Issue-Vorlagen (Fehler, Wunsch, Plugin-Vorschlag) als Markdown, PR-Vorlage mit Definition of Done.

Die Doku-Webseite wird aus `docs/` mit einem statischen Generator gebaut und auf GitHub Pages veröffentlicht. Sie enthält Schnellstart, Profil-Leitfaden für Teams, Plugin-Leitfaden mit Tutorial („erstes Plugin in 30 Minuten“), Referenz aller Schemas (aus den Schemas erzeugt), API-Referenz (aus OpenAPI erzeugt) und CLI-Referenz (aus cobra erzeugt).

## Übernahme aus DevBox 2

DevBox 2 (Bash-Skripte mit Node-Oberfläche) bleibt bis zur Version 1.0 nutzbar. Der Umstieg passiert mit einem Befehl und ohne Neuinstallation der Laufzeiten, weil die Installationsorte gleich bleiben (`~/.local/share/node`, `~/.local/share/jdk`, `~/.local/share/dotnet`, uv-Verzeichnisse).

`devbox migrate --from-v2` liest die alte Benutzerkonfiguration (Schlüssel=Wert) und die Team-Vorgaben der alten Installation, zeigt einen Plan und schreibt nach Bestätigung:

| DevBox 2 | DevBox 1.0 |
| --- | --- |
| Git-Server-Adresse, Benutzername | Zugangs-Instanz vom Typ `git-host` |
| Registry-Adresse, Gruppen für npm, Maven, NuGet, PyPI, Docker | Zugangs-Instanz vom Typ `registry` mit Abnehmern und ihren Optionen |
| npm-Scope | Option `scopes` des Abnehmers `npm` |
| Tokens aus Schlüsselbund oder Datei | Tresor, danach einmal Anwenden aller Abnehmer |
| Projektordner, Aufräum-Alter | `cleanup.projectRoots`, `cleanup.olderThanDays` |
| xrdp-Werte (Port, Farbtiefe, Sicherheit, Ton) | `xrdp.*` |
| Java-Standardversion, Node-Kanal | `java.defaultVersion`, `node.channel` |
| markierte Blöcke in `.npmrc`, `settings.xml`, `pip.conf`, `NuGet.Config` | werden erkannt und auf die neuen Markierungen umgestellt, nicht verdoppelt |

Mit `--emit-profile profile.json` erzeugt derselbe Befehl aus den alten Team-Vorgaben ein Profil, das ein Team in sein eigenes Repository legen kann. So wird aus einer fest eingebauten Vorgabe eine austauschbare Datei. Ein Testfall mit einer vollständigen DevBox-2-Konfiguration (nur Beispieladressen) liegt unter `test/fixtures/v2/`.

## Fahrplan

Fünf Phasen, jede endet an einem Tor mit prüfbaren Kriterien. Termine hängen an der verfügbaren Zeit; die Reihenfolge nicht.

| Tor | Kriterien |
| --- | --- |
| Schemas stehen | alle `*.v1.json` und `openapi.json` im Repository, mit gültigen und ungültigen Beispielen getestet; CI, Lizenz, Pflichtdateien, `CLAUDE.md` vorhanden |
| Plugin-API eingefroren | Kern mit CLI, Plugin-Host, Runner, Tresor, Konfiguration und Profilen; Plugin `java` vollständig über das Protokoll; ab hier nur noch Ergänzungen an `plugin.v1` |
| Gleichstand mit DevBox 2 | alle 20 Plugins, Oberfläche auf neuer API, Migration, Container-Matrix grün |
| Version 1.0 | Beta mit mindestens drei Teams und eigenen Profilen, Doku-Webseite, Pakete und Paketquellen, Katalog mit mindestens drei Plugins von außerhalb des Kerns, keine offenen Fehler der Stufe hoch |

&#91;embedded content: Fahrplan · 5 Phasen, 4 Tore\]

## Umsetzung mit Claude Code

Das Projekt wird in kleinen, geprüften Arbeitspaketen gebaut. Ein Paket ist ein Branch und ein Pull Request. Claude Code bekommt zu jedem Paket diesen Abschnitt, die verlinkten Abschnitte dieses Konzepts und die `CLAUDE.md` im Repository. Sprache im Repository (Code, Kommentare, Commits, Doku) ist Englisch, damit das Projekt international beitragsfähig ist; die Oberfläche kommt mit Deutsch und Englisch.

**Arbeitsweise je Paket**

1. Plan-Modus: Claude Code liest die betroffenen Schemas und Pakete und legt einen Plan mit Dateiliste vor.
2. Zuerst Schema und OpenAPI, dann `make generate`, dann Tests, dann Code.
3. `make lint test` muss lokal grün sein, bevor ein Commit entsteht.
4. PR-Beschreibung mit erfüllten Akzeptanzkriterien als Checkliste.

**`CLAUDE.md`** im Wurzelverzeichnis (Startfassung):

```markdown
# DevBox – working agreement for Claude Code

DevBox sets up and maintains Linux developer machines. One Go binary (`devbox`)
with an embedded Angular UI; every capability is a plugin (`plugins/<id>/plugin.json`).

## Golden rules
- Nothing is hard-coded: no hostnames, URLs, registry names, versions or paths in code.
  Every value is a plugin setting with a default in plugin.json. Defaults point to public
  sources or are empty. Use example.com in tests, docs and fixtures.
- JSON everywhere. Every file DevBox reads or writes has a schema in `schema/*.v1.json`.
  Schemas are the source of truth; never edit `internal/gen/` or `web/src/app/gen/` by hand.
  Allowed non-JSON files: .github/workflows/*.yml, .goreleaser.yaml, eslint.config.mjs,
  go.mod, Makefile. Do not add others.
- Strict parsing: unknown keys are errors (except `$schema`, `$comment`).
- The core knows no language or tool. If a plugin needs something new, extend the protocol.
- Idempotent actions, dry-run support (`DEVBOX_DRY_RUN=1`), backups before file changes,
  marked blocks (`# >>> devbox:<plugin> >>>`) instead of appends.
- Secrets only via the request file; never in env vars, argv, logs or events.
- UI and CLI texts are neutral (no du/Sie/ich) and exist in de.json and en.json.

## Commands
- make generate   # types from schema/ and api/openapi.json
- make dev        # core + ng serve
- make lint test  # must pass before every commit
- make e2e        # Playwright in a test container

## Layout
cmd/devbox, internal/{api,catalog,cli,config,history,platform,plugin,profile,runner,
schema,sdk,tui,vault,gen}, schema/, api/openapi.json, plugins/, web/, sdk/ts/,
templates/plugin/, packaging/, test/, docs/

## Go
Go 1.23+, CGO_ENABLED=0, log/slog, context everywhere, wrap errors with %w,
no panics outside main, table-driven tests, testscript for CLI flows.

## Web
Angular standalone + signals + OnPush, zoneless, zenit-ui components only,
no `any`, one WebSocket via the Sessions store, forms via SchemaForm.

## Plugins
Bash with `set -euo pipefail`, shellcheck clean, only `devbox sdk` and commands
listed in requires.commands. Checks print one JSON object to stdout. Actions write
JSON Lines to fd 3. Each plugin has bats tests; `devbox plugin test` must pass.

## Commits
Conventional Commits, DCO sign-off (`git commit -s`), one work package per PR.
```

**Arbeitspakete**

| Paket | Inhalt | Akzeptanzkriterien | Braucht |
| --- | --- | --- | --- |
| AP-01 Gerüst | `go.mod`, `Makefile`, npm-Workspaces, CI (lint, test), Pflichtdateien, `CLAUDE.md`, Renovate, release-please | `make lint test build` grün auf leerem Stand; CI läuft auf PRs; DCO-Check aktiv | – |
| AP-02 Schemas | alle `schema/*.v1.json`, Fixtures gültig und ungültig, `make generate` | jedes JSON-Beispiel dieses Konzepts validiert; je Schema mindestens drei ungültige Fixtures mit erwartetem Fehlerpfad | 01 |
| AP-03 Konfiguration | `internal/config`: sechs Ebenen, Platzhalter, Sperren, Herkunft; `devbox config` | Tabellentests für jede Ebene, Zyklus erkannt mit Pfad, gesperrter Wert liefert `setting-locked`, `explain --json` wie im Beispiel | 02 |
| AP-04 Plugin-Host | Suche, Validierung, Zustände, Voraussetzungen, Abhängigkeiten, Ereignisse | `devbox plugin list --json` zeigt für Test-Plugins alle fünf Zustände mit Grund; doppelte ID ergibt Hinweis, erster Fund gewinnt | 02, 03 |
| AP-05 Runner | PTY, Deskriptor 3, Anfrage-Datei, Umgebung, Exit-Codes, Abbruch, Zeitgrenzen, Sperren, Maskierung | testscript je Exit-Code; Abbruch beendet Prozessgruppe in unter 11 s; Geheimnis taucht in keinem Log und keinem Ereignis auf | 04 |
| AP-06 Verlauf | JSON Lines, Ausgabe-Dateien, Sicherungen, Aufbewahrung | Sitzung erzeugt genau eine Verlaufszeile; Aufbewahrung löscht nach `keepDays` und `keepSessions` | 05 |
| AP-07 Tresor | Secret Service, age-Datei, Wechsel zwischen beiden | Tests mit Fake-Keyring; Datei 0600; Wechsel verliert keinen Eintrag | 02 |
| AP-08 SDK-Befehle | `devbox sdk param, setting, secret, emit, block, block-remove, json-set, download` | `block` zweimal ergibt keinen Diff; im Probelauf bleibt jede Datei unverändert; `json-set` erhält Formatierung | 05 |
| AP-09 REST-API | Endpunkte, OpenAPI-Prüfung, Fehlerformat, CSRF, Unix-Socket, PAM-Login | jede Antwort im Test gegen `openapi.json` validiert; Anfrage ohne Kopfzeile ergibt `csrf`; Login-Ratenbegrenzung getestet | 03–07 |
| AP-10 WebSocket | Themen, Ausgabe mit `seq`, Wiederaufnahme, Eingabe, Größe | Neuverbinden mit `fromSeq` liefert lückenlose Ausgabe; zwei Fenster sehen dieselbe Sitzung | 09 |
| AP-11 CLI | alle Befehle aus dem Abschnitt CLI, `--json`, Exit-Codes, Vervollständigung | testscript je Befehl; `--json`-Ausgaben gegen Schemas geprüft | 09 |
| AP-12 Profile | Quellen Datei, HTTPS, Git; Cache; optionale Signaturpflicht | Profil aus lokalem Git-Repository im Test; ungültige Signatur wird abgelehnt mit klarer Meldung | 03 |
| AP-13 Referenz-Plugin | `plugins/java`, Vorlage `templates/plugin/bash`, `devbox plugin new, validate, test` | `java` besteht `devbox plugin test`; Probelauf meldet `change`-Nachrichten; zweiter Lauf meldet keine Änderung | 05, 08 |
| AP-14 Oberfläche Basis | Shell mit zenit-ui, Stores, WebSocket, Übersicht, Terminal-Dock, i18n, Theme | Playwright: Übersicht lädt in unter 1 s mit zwischengespeicherten Prüfungen; Aktion starten und Ausgabe sehen; axe ohne Verstöße | 10 |
| AP-15 SchemaForm | Formulare aus Schemas, Einstellungsseite, Plugin-Detailseite | Vitest für jeden Schema-Typ; Fehler per `pointer` am richtigen Feld; gesperrte Felder schreibgeschützt mit Profilname | 14 |
| AP-16 Zugangsdaten | Typen `registry`, `git-host`, `ssh-key`; Abnehmer-Ablauf; Ablaufwarnung; Seiten | Token setzen startet Prüfen und Anwenden; Fehler eines Abnehmers stoppt die anderen nicht; Warnung ab `warnDays` | 07, 13, 15 |
| AP-17 Laufzeiten | Plugins `system`, `base`, `node`, `dotnet`, `python` | alle bestehen `devbox plugin test`; Quellen per Einstellung auf Testserver umleitbar | 13 |
| AP-18 Werkzeuge | Plugins `docker`, `git`, `githost`, `registry`, `npm`, `maven`, `gradle`, `nuget`, `pip` | Abnehmer gegen Fake-Registry im Test; `env`- und `file`-Modus getestet; Entfernen hinterlässt Dateien wie vorher | 16 |
| AP-19 Rest | Plugins `xrdp`, `cleanup`, `templates` | xrdp-Konfiguration gegen Test-INI-Dateien; Aufräumen im Probelauf mit Größen, ohne Löschung | 13 |
| AP-20 Einrichtung | Ersteinrichtung und Gruppen in Kern, API, CLI, Oberfläche | `devbox setup --yes` mit Testprofil im Container bis zum Ende; Abbruch und Fortsetzen ab offenem Schritt | 17–19 |
| AP-21 Terminal-Menü | Bubble Tea, alle Bereiche, Formulare aus Schemas | Bedienung im testscript per simulierter Eingabe; läuft in 80×24 | 11, 15 |
| AP-22 Migration | `devbox migrate --from-v2`, `--emit-profile` | Fixture aus `test/fixtures/v2/` ergibt erwartete Konfiguration und erwartetes Profil Byte für Byte | 16–19 |
| AP-23 Katalog | Katalog-Index, Installation, Signaturprüfung, Sperrdatei, Rechte-Bestätigung | unsigniertes oder falsch signiertes Plugin wird abgelehnt; neue Rechte bei Update erfordern Bestätigung | 04 |
| AP-24 Sandbox | `systemd-run`, `bubblewrap`, Rechte durchsetzen | Schreibversuch außerhalb angemeldeter Pfade schlägt im Test fehl; ohne beide Werkzeuge klare Warnung | 05 |
| AP-25 Pakete | GoReleaser, `.deb`, `.rpm`, AUR, `install.sh`, Paketquellen, Container-Matrix | Installation und `devbox setup --yes` auf allen vier Distributionen in CI grün; Namenskonflikt-Fall getestet | 20 |
| AP-26 Doku | Webseite aus `docs/`, Referenzen aus Schemas, OpenAPI und cobra | Doku-Build in CI; jede Schema-Eigenschaft und jeder Befehl in der Referenz | 11, 02 |
| AP-27 Repositories | Schlüssel repositories in config und profile, Arten plugins, catalog, profile, templates, packages; devbox repo, API, Seite in den Einstellungen; Paketquellen im Plugin base; Vorlage templates/repository | Plugin-Repository aus lokalem Git im Test synchronisiert und Plugins erscheinen; neue Rechte bei sync erfordern Bestätigung; apt- und rpm-Quelle im Container angelegt und wieder entfernt ohne Rückstände; when filtert nach Distribution | 04, 12, 17, 23 |
| AP-28 Sprachpakete | contributes.language, Nachschlage-Reihenfolge im Kern und in der Oberfläche, Plugins lang-de und lang-en, Sprachschritt am Anfang der Ersteinrichtung, devbox lang, Abdeckung in plugin validate | Test-Sprachpaket mit halber Abdeckung fällt pro Schlüssel sauber auf Englisch zurück; Sprachwechsel ohne Neuladen der Seite; Auswahl zeigt jedes installierte Paket; lang-de und lang-en lassen sich nicht entfernen; keine Zeichenkette für Benutzer im Go- oder TypeScript-Code (Lint-Regel) | 04, 14, 20 |

**Vorlage für den Auftrag an Claude Code**

```text
Umsetzen: AP-05 Runner aus docs/concept (Abschnitte Plugin-Protokoll und Sicherheit).
Vorher lesen: CLAUDE.md, schema/event.v1.json, schema/request.v1.json, internal/plugin.
Zuerst Plan mit Dateiliste vorlegen. Akzeptanzkriterien aus der Tabelle als Checkliste
in die PR-Beschreibung, jeden Punkt mit Testnamen belegen.
```

## Offene Entscheidungen

| Frage | Vorschlag | Bis wann |
| --- | --- | --- |
| Domain für Doku, Schemas und Paketquellen | `devbox.zenit-hosting.de` mit `pkg.`-Unterdomain | vor AP-02, weil die Schema-Adressen darauf zeigen |
| Sprache im Repository | Englisch für Code, Commits und Doku, Oberfläche deutsch und englisch | vor AP-01 |
| Netz-Sandbox in 1.0 oder 1.1 | 1.1: in 1.0 anzeigen und protokollieren, erzwingen erst danach | vor AP-24 |
| Mitgelieferte Plugins im Binary oder nur im Paket | beides: eingebettet für `install.sh`, als Dateien im Paket für Distributionen | vor AP-25 |
| Statischer Generator für die Doku | ein Generator mit JSON- oder JS-Konfiguration, damit keine YAML-Datei nötig ist | vor AP-26 |
| Erste Beta-Teams | drei Teams mit unterschiedlichen Registries (Nexus, Artifactory, Gitea) | vor dem Tor „Gleichstand“ |

## Nächste Schritte

- [ ] Repository `zenit-hosting/devbox` anlegen (Apache-2.0, Branch-Schutz, DCO-App)
- [ ] Repository `zenit-hosting/devbox-plugins` anlegen (leer, mit Katalog-Index)
- [ ] Domain festlegen und GitHub Pages einrichten
- [ ] Offene Entscheidungen oben treffen
- [ ] Dieses Konzept als `docs/concept.md` ins Repository übernehmen
- [ ] AP-01 und AP-02 mit Claude Code umsetzen, danach Review der Schemas als erstes Tor
- [ ] Beispiel-Profil mit `example.com`-Adressen als `docs/examples/profile.json` anlegen

## Anhang: JSON-Schemas

Startfassungen der beiden wichtigsten Schemas. AP-02 vervollständigt sie und ergänzt die übrigen nach demselben Muster: `$defs` für wiederkehrende Teile, `additionalProperties: false` überall, `$schema` und `$comment` als erlaubte Zusatzschlüssel.

**`schema/plugin.v1.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://devbox.zenit-hosting.de/schema/plugin.v1.json",
  "title": "DevBox plugin manifest",
  "type": "object",
  "required": ["id", "version", "name"],
  "additionalProperties": false,
  "properties": {
    "$schema": { "type": "string" },
    "$comment": { "type": "string" },
    "protocol": { "const": 1, "default": 1 },
    "id": { "$ref": "#/$defs/id" },
    "version": { "type": "string", "pattern": "^\\d+\\.\\d+\\.\\d+(-[0-9A-Za-z.-]+)?$" },
    "name": { "$ref": "#/$defs/text" },
    "description": { "$ref": "#/$defs/text" },
    "category": { "enum": ["core", "system", "runtime", "tool", "credentials", "consumer", "projects", "remote", "maintenance", "language", "other"] },
    "icon": { "type": "string", "pattern": "^[a-z0-9_]+$" },
    "license": { "type": "string" },
    "authors": { "type": "array", "items": { "type": "string" } },
    "homepage": { "type": "string", "format": "uri" },
    "requires": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "devbox": { "type": "string" },
        "os": { "type": "array", "items": { "enum": ["linux"] } },
        "arch": { "type": "array", "items": { "enum": ["amd64", "arm64"] } },
        "distro": { "type": "array", "items": { "type": "string" } },
        "commands": { "type": "array", "items": { "type": "string" } },
        "plugins": { "type": "object", "propertyNames": { "$ref": "#/$defs/id" }, "additionalProperties": { "type": "string" } }
      }
    },
    "permissions": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "network": { "type": "array", "items": { "type": "string" } },
        "files": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "read": { "type": "array", "items": { "type": "string" } },
            "write": { "type": "array", "items": { "type": "string" } }
          }
        },
        "sudo": { "type": "boolean", "default": false },
        "credentials": { "type": "array", "items": { "$ref": "#/$defs/id" } }
      }
    },
    "settings": { "type": "object", "propertyNames": { "$ref": "#/$defs/key" }, "additionalProperties": { "$ref": "#/$defs/field" } },
    "checks": { "type": "object", "propertyNames": { "$ref": "#/$defs/key" }, "additionalProperties": { "$ref": "#/$defs/check" } },
    "actions": { "type": "object", "propertyNames": { "$ref": "#/$defs/key" }, "additionalProperties": { "$ref": "#/$defs/action" } },
    "credentialTypes": { "type": "object", "propertyNames": { "$ref": "#/$defs/id" }, "additionalProperties": { "$ref": "#/$defs/credentialType" } },
    "consumes": { "type": "object", "propertyNames": { "$ref": "#/$defs/id" }, "additionalProperties": { "$ref": "#/$defs/consumer" } },
    "contributes": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "groups": { "type": "object", "additionalProperties": { "type": "array", "items": { "$ref": "#/$defs/ref" } } },
        "setup": { "type": "array", "items": { "type": "object", "required": ["action"], "additionalProperties": false, "properties": { "action": { "$ref": "#/$defs/ref" }, "after": { "type": "array", "items": { "$ref": "#/$defs/ref" } } } } },
        "language": { "type": "object", "required": ["code", "name", "messages"], "additionalProperties": false, "properties": { "code": { "type": "string", "pattern": "^[a-z]{2,3}(-[A-Z]{2})?$" }, "name": { "$ref": "#/$defs/text" }, "englishName": { "$ref": "#/$defs/text" }, "fallback": { "type": "string" }, "messages": { "type": "string" } } },
        "overview": { "type": "object", "additionalProperties": false, "properties": { "check": { "$ref": "#/$defs/ref" }, "primaryAction": { "$ref": "#/$defs/ref" } } },
        "links": { "type": "array", "items": { "type": "object", "required": ["label", "url"], "additionalProperties": false, "properties": { "label": { "$ref": "#/$defs/text" }, "url": { "type": "string" } } } }
      }
    },
    "on": { "type": "object", "additionalProperties": { "$ref": "#/$defs/command" } },
    "i18n": { "type": "object", "additionalProperties": false, "properties": { "default": { "type": "string" }, "dir": { "type": "string" } } }
  },
  "$defs": {
    "id": { "type": "string", "pattern": "^[a-z][a-z0-9-]{1,31}$" },
    "key": { "type": "string", "pattern": "^[a-z][A-Za-z0-9]{0,47}$" },
    "ref": { "type": "string", "pattern": "^[a-z][a-z0-9-]{1,31}\\.[a-z][A-Za-z0-9]{0,47}$" },
    "text": { "type": "string", "minLength": 1, "maxLength": 500 },
    "duration": { "type": "string", "pattern": "^\\d+(ms|s|m|h)$" },
    "command": { "type": "array", "minItems": 1, "items": { "type": "string" } },
    "field": {
      "type": "object",
      "required": ["type", "label"],
      "properties": {
        "type": { "enum": ["string", "integer", "number", "boolean", "array", "object"] },
        "label": { "$ref": "#/$defs/text" },
        "help": { "$ref": "#/$defs/text" },
        "default": true,
        "advanced": { "type": "boolean" },
        "secret": { "type": "boolean" },
        "optionsFrom": { "type": "string" }
      },
      "$comment": "Weitere JSON-Schema-Schlüsselwörter wie enum, format, pattern, minimum, items sind erlaubt; AP-02 listet sie abschließend auf."
    },
    "check": {
      "type": "object",
      "required": ["label", "run"],
      "additionalProperties": false,
      "properties": {
        "label": { "$ref": "#/$defs/text" },
        "run": { "$ref": "#/$defs/command" },
        "timeout": { "$ref": "#/$defs/duration" },
        "cache": { "$ref": "#/$defs/duration" },
        "network": { "type": "boolean" },
        "refreshOn": { "type": "array", "items": { "type": "string" } }
      }
    },
    "action": {
      "type": "object",
      "required": ["label", "run"],
      "additionalProperties": false,
      "properties": {
        "label": { "$ref": "#/$defs/text" },
        "description": { "$ref": "#/$defs/text" },
        "run": { "$ref": "#/$defs/command" },
        "params": { "type": "object", "propertyNames": { "$ref": "#/$defs/key" }, "additionalProperties": { "$ref": "#/$defs/field" } },
        "timeout": { "$ref": "#/$defs/duration" },
        "confirm": { "oneOf": [{ "type": "boolean" }, { "$ref": "#/$defs/text" }] },
        "danger": { "type": "boolean" },
        "sudo": { "type": "boolean" },
        "locks": { "type": "array", "items": { "type": "string" } }
      }
    },
    "credentialType": {
      "type": "object",
      "required": ["label", "fields", "secret"],
      "additionalProperties": false,
      "properties": {
        "label": { "$ref": "#/$defs/text" },
        "fields": { "type": "object", "additionalProperties": { "$ref": "#/$defs/field" } },
        "secret": { "type": "object", "required": ["label"], "additionalProperties": false, "properties": { "label": { "$ref": "#/$defs/text" }, "expires": { "enum": ["never", "optional", "required"] } } },
        "verify": { "$ref": "#/$defs/ref" }
      }
    },
    "consumer": {
      "type": "object",
      "required": ["apply", "remove"],
      "additionalProperties": false,
      "properties": {
        "apply": { "$ref": "#/$defs/command" },
        "remove": { "$ref": "#/$defs/command" },
        "options": { "type": "object", "additionalProperties": { "$ref": "#/$defs/field" } }
      }
    }
  }
}
```

Das vollständige `java`-Manifest aus dem Abschnitt Plugin-System erfüllt dieses Schema und wird in AP-02 als gültiges Fixture übernommen. Die Ausschnitte für `registry` und `npm` zeigen nur die jeweils relevanten Schlüssel; als echte Manifeste brauchen sie zusätzlich `version` und `name`. Platzhalter wie `${java.defaultVersion}` sind in jedem Feld erlaubt und werden vor der Typprüfung aufgelöst.

**`schema/config.v1.json`** (Auszug der Struktur):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://devbox.zenit-hosting.de/schema/config.v1.json",
  "title": "DevBox configuration",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "$schema": { "type": "string" },
    "$comment": { "type": "string" },
    "profiles": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["source"],
        "additionalProperties": false,
        "properties": {
          "source": { "type": "string", "pattern": "^(file|https|git\\+https|git\\+ssh)://" },
          "ref": { "type": "string" },
          "path": { "type": "string" },
          "trust": { "$ref": "#/$defs/trust" }
        }
      }
    },
    "plugins": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "paths": { "type": "array", "items": { "type": "string" } },
        "disabled": { "type": "array", "items": { "type": "string" } }
      }
    },
    "settings": { "type": "object", "propertyNames": { "pattern": "^[a-z][a-z0-9-]{1,31}\\.[a-z][A-Za-z0-9]{0,47}$" } },
    "repositories": { "type": "object", "propertyNames": { "pattern": "^[a-z][a-z0-9-]{1,31}$" }, "additionalProperties": { "$ref": "#/$defs/repository" } },
    "server": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "listen": { "type": "string", "default": "127.0.0.1:7878" },
        "auth": { "enum": ["local", "pam", "off"], "default": "local" },
        "tls": { "type": "object", "additionalProperties": false, "properties": { "cert": { "type": "string" }, "key": { "type": "string" } } },
        "allowedOrigins": { "type": "array", "items": { "type": "string", "format": "uri" } }
      }
    },
    "ui": { "type": "object", "additionalProperties": false, "properties": { "language": { "type": "string", "pattern": "^[a-z]{2,3}(-[A-Z]{2})?$", "$comment": "muss zu einem installierten Sprachpaket passen; geprüft beim Laden, nicht im Schema" }, "scheme": { "enum": ["system", "light", "dark", "contrast"] }, "accent": { "type": "string" } } },
    "vault": { "type": "object", "additionalProperties": false, "properties": { "backend": { "enum": ["auto", "keyring", "age"] }, "passphrase": { "type": "boolean" } } },
    "history": { "type": "object", "additionalProperties": false, "properties": { "keepDays": { "type": "integer", "minimum": 1 }, "keepSessions": { "type": "integer", "minimum": 10 } } },
    "network": { "type": "object", "additionalProperties": false, "properties": { "proxy": { "type": "string" }, "noProxy": { "type": "string" }, "caFile": { "type": "string" } } }
  },
  "$defs": {
    "repository": {
      "type": "object",
      "required": ["kind"],
      "additionalProperties": false,
      "properties": {
        "kind": { "enum": ["plugins", "catalog", "profile", "templates", "packages"] },
        "source": { "type": "string", "pattern": "^(file|https|git\\+https|git\\+ssh)://" },
        "ref": { "type": "string" },
        "path": { "type": "string" },
        "credential": { "type": "string" },
        "trust": { "oneOf": [{ "enum": ["prompt", "pinned"] }, { "$ref": "#/$defs/trust" }], "default": "prompt" },
        "format": { "enum": ["apt", "rpm", "pacman", "zypper"] },
        "url": { "type": "string" },
        "suite": { "type": "string" },
        "components": { "type": "array", "items": { "type": "string" } },
        "key": { "type": "string" },
        "when": { "type": "object", "additionalProperties": false, "properties": { "distro": { "type": "array", "items": { "type": "string" } }, "arch": { "type": "array", "items": { "enum": ["amd64", "arm64"] } } } },
        "locked": { "type": "boolean" },
        "enabled": { "type": "boolean", "default": true }
      },
      "$comment": "AP-27 ergänzt if/then: packages verlangt format, url und key; alle anderen Arten verlangen source."
    },
    "trust": {
      "type": "object",
      "required": ["issuer", "identity"],
      "additionalProperties": false,
      "properties": { "issuer": { "type": "string", "format": "uri" }, "identity": { "type": "string" } }
    }
  }
}
```
