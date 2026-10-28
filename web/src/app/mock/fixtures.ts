// Typed fixtures of the mock backend. They follow the OpenAPI contract (the `satisfies`
// clauses make the compiler check every object against it). Hosts use example.com.

import type {
  CheckSummary,
  CredentialInstance,
  Info,
  Manifest,
  Messages,
  PluginDetail,
  Session,
} from "../api/types";

export const infoFixture = {
  vendor: "zenit-hosting",
  version: "0.2.0-mock",
  commit: "abc1234",
  date: "2026-01-01T00:00:00Z",
  protocol: 1,
  auth: "off",
  host: {
    name: "devbox-mock",
    codename: "noble",
    user: "dev",
    distro: "ubuntu",
    distroVersion: "24.04",
    arch: "amd64",
    desktop: "xfce",
  },
  home: "/home/dev",
  features: ["ui", "ws", "socket"],
  ui: { scheme: "system" },
} satisfies Info;

const javaManifest = {
  id: "java",
  version: "1.0.0",
  name: "Java",
  description: "Install, switch and update JDKs",
  category: "runtime",
  icon: "coffee",
  settings: {
    vendorApi: {
      type: "string",
      format: "uri",
      default: "https://api.example.com/v3",
      label: "Release API",
      help: "Adoptium-compatible API. Change it to use a mirror in the own network.",
    },
    defaultVersion: {
      type: "integer",
      enum: [8, 11, 17, 21, 25],
      default: 21,
      label: "Default JDK",
    },
    installDir: {
      type: "string",
      format: "path",
      default: "${xdg:data}/jdk",
      label: "Install location",
      advanced: true,
    },
    keepPatches: {
      type: "integer",
      minimum: 1,
      maximum: 5,
      default: 1,
      label: "Patch versions to keep per JDK",
    },
    envFile: {
      type: "string",
      format: "path",
      default: "${xdg:config}/devbox/env.d/java.sh",
      label: "Shell environment file",
      help: "Sets JAVA_HOME and PATH for new shells.",
      advanced: true,
    },
  },
  checks: {
    installed: { label: "Installed JDKs" },
    latest: { label: "New versions", network: true },
  },
  actions: {
    install: {
      label: "Install JDK",
      params: {
        version: {
          type: "integer",
          enum: [8, 11, 17, 21, 25],
          default: "${java.defaultVersion}",
          label: "Version",
        },
        makeDefault: { type: "boolean", default: true, label: "Make it the default" },
      },
      timeout: "15m",
    },
    update: { label: "Update and clean up", timeout: "30m" },
    use: {
      label: "Switch default JDK",
      params: {
        version: {
          type: "string",
          label: "Version",
          optionsFrom: "java.installed#/data/items/*/version",
        },
      },
    },
    remove: {
      label: "Remove JDK",
      params: {
        version: {
          type: "string",
          label: "Version",
          optionsFrom: "java.installed#/data/items/*/version",
        },
      },
      confirm:
        "The JDK is deleted. Projects that use it via JAVA_HOME need another one afterwards.",
      danger: true,
    },
  },
  contributes: { overview: { check: "java.installed", primaryAction: "java.update" } },
  i18n: { default: "en", dir: "i18n" },
} satisfies Manifest;

const nodeManifest = {
  id: "node",
  version: "1.0.0",
  name: "Node.js",
  description: "Install Node.js, switch versions, carry global packages along",
  category: "runtime",
  icon: "javascript",
  settings: {
    channel: {
      type: "string",
      enum: ["lts", "current", "latest"],
      default: "lts",
      label: "Release channel",
    },
    distUrl: {
      type: "string",
      format: "uri",
      default: "https://nodejs.example.com/dist",
      label: "Download address",
      help: "Mirror of the Node.js distribution.",
    },
    globalPackages: {
      type: "array",
      items: { type: "string", pattern: "^[a-z@][a-z0-9@/._-]*$" },
      default: [],
      label: "Global packages",
      help: "Installed again after every Node.js update.",
    },
    registryToken: {
      type: "string",
      secret: true,
      label: "Registry token",
      advanced: true,
    },
  },
  checks: {
    installed: { label: "Installed versions" },
    latest: { label: "New versions", network: true },
  },
  actions: {
    install: {
      label: "Install Node.js",
      params: {
        version: { type: "string", label: "Version", pattern: "^[0-9]+(\\.[0-9]+){0,2}$" },
      },
    },
    update: { label: "Update", timeout: "10m" },
  },
  contributes: { overview: { check: "node.installed", primaryAction: "node.update" } },
  i18n: { default: "en", dir: "i18n" },
} satisfies Manifest;

const systemManifest = {
  id: "system",
  version: "1.0.0",
  name: "System",
  description: "Machine facts and updates of DevBox itself",
  category: "core",
  icon: "computer",
  settings: {
    updateChannel: {
      type: "string",
      enum: ["stable", "beta"],
      default: "stable",
      label: "Update channel",
    },
  },
  checks: { host: { label: "Machine" } },
  actions: {
    selfUpdate: {
      label: "Update DevBox",
      description: "Downloads the latest release and replaces the binary.",
      sudo: true,
    },
  },
  contributes: { overview: { check: "system.host", primaryAction: "system.selfUpdate" } },
  i18n: { default: "en", dir: "i18n" },
} satisfies Manifest;

const dockerManifest = {
  id: "docker",
  version: "1.0.0",
  name: "Docker",
  description: "Docker Engine, group membership and daemon settings",
  category: "tool",
  icon: "deployed_code",
  settings: {
    registryMirrors: {
      type: "array",
      items: { type: "string", format: "uri" },
      default: [],
      label: "Registry mirrors",
    },
  },
  checks: { installed: { label: "Docker Engine" } },
  actions: { install: { label: "Install Docker" } },
  contributes: { overview: { check: "docker.installed", primaryAction: "docker.install" } },
  i18n: { default: "en", dir: "i18n" },
} satisfies Manifest;

export const pluginsFixture = [
  {
    id: "docker",
    name: "Docker",
    version: "1.0.0",
    category: "tool",
    state: "blocked",
    reasons: [
      { code: "requires-command", detail: "systemctl" },
      { code: "requires-plugin", detail: "base >=1.0.0" },
    ],
    path: "/usr/share/devbox/plugins/docker",
    manifest: dockerManifest,
  },
  {
    id: "java",
    name: "Java",
    version: "1.0.0",
    category: "runtime",
    state: "ready",
    reasons: [],
    path: "/usr/share/devbox/plugins/java",
    manifest: javaManifest,
    resolvedDefaults: { install: { version: 21, makeDefault: true } },
  },
  {
    id: "node",
    name: "Node.js",
    version: "1.0.0",
    category: "runtime",
    state: "ready",
    reasons: [],
    path: "/usr/share/devbox/plugins/node",
    manifest: nodeManifest,
    resolvedDefaults: { install: {} },
  },
  {
    id: "system",
    name: "System",
    version: "1.0.0",
    category: "core",
    state: "ready",
    reasons: [],
    path: "/usr/share/devbox/plugins/system",
    manifest: systemManifest,
    resolvedDefaults: { selfUpdate: {} },
  },
] satisfies PluginDetail[];

const checkedAt = "2026-03-02T08:30:00Z";

export const checksFixture = [
  {
    id: "java.installed",
    label: "Installed JDKs",
    status: "ok",
    summary: "2 JDKs installed, default 21",
    checkedAt,
    details: [
      { label: "Default", value: "21.0.5" },
      { label: "Install location", value: "/home/dev/.local/share/jdk" },
    ],
    hints: [],
    data: { items: [{ version: "21.0.5" }, { version: "17.0.13" }] },
  },
  {
    id: "java.latest",
    label: "New versions",
    status: "warn",
    summary: "Update available: 21.0.6",
    checkedAt,
    details: [{ label: "Newest", value: "21.0.6" }],
    hints: [{ text: "A newer patch version of JDK 21 is available.", action: "java.update" }],
    data: {},
  },
  {
    id: "node.installed",
    label: "Installed versions",
    status: "ok",
    summary: "Node.js 22.12.0",
    checkedAt,
    details: [{ label: "Active", value: "22.12.0" }],
    hints: [],
    data: { items: [{ version: "22.12.0" }] },
  },
  {
    id: "node.latest",
    label: "New versions",
    status: "ok",
    summary: "Up to date",
    checkedAt,
    data: {},
  },
  {
    id: "system.host",
    label: "Machine",
    status: "ok",
    summary: "Ubuntu 24.04, 16 GiB memory",
    checkedAt,
    details: [
      { label: "Kernel", value: "6.8.0" },
      { label: "Desktop", value: "XFCE" },
    ],
    data: {},
  },
  {
    id: "docker.installed",
    label: "Docker Engine",
    status: "missing",
    summary: "Docker is not installed",
    checkedAt,
    hints: [{ text: "Install Docker Engine to build containers.", action: "docker.install" }],
    data: {},
  },
] satisfies CheckSummary[];

export interface SettingSeed {
  plugin: string;
  name: string;
  /** Value set in a profile; the setting is then locked. */
  lockedBy?: string;
  lockedValue?: unknown;
  /** Value of the user layer. */
  userValue?: unknown;
}

export const settingSeeds: SettingSeed[] = [
  { plugin: "java", name: "keepPatches", lockedBy: "profile:example-team", lockedValue: 2 },
  { plugin: "java", name: "defaultVersion", userValue: 17 },
  { plugin: "node", name: "distUrl", userValue: "https://mirror.example.com/node" },
];

const gitCredential = {
  name: "git-example",
  type: "git-host",
  origin: "user",
  label: "Git server example.com",
  fields: { url: "https://git.example.com", user: "dev" },
  locked: [],
  consumers: { git: { helper: true } },
  secret: {
    ref: "keyring:git-example",
    updatedAt: "2026-02-01T10:00:00Z",
    expiresAt: "2026-06-30T00:00:00Z",
  },
  verified: { at: "2026-02-01T10:01:00Z", status: "ok" },
  applied: { git: "2026-02-01T10:02:00Z" },
} satisfies CredentialInstance;

const teamCredential = {
  name: "registry-team",
  type: "registry",
  origin: "profile:example-team",
  label: "Team registry",
  fields: { url: "https://registry.example.com" },
  locked: ["url"],
  consumers: { npm: {}, maven: {} },
  verified: { at: "2026-02-02T09:00:00Z", status: "failed", message: "HTTP 401" },
  applied: {},
} satisfies CredentialInstance;

export const credentialsFixture: CredentialInstance[] = [gitCredential, teamCredential];

/** Translation files of plugins, by plugin and language. Missing entries answer 404. */
export const messagesFixture: Record<string, Messages> = {
  "java/de": {
    "plugin.name": "Java",
    "plugin.description": "JDKs installieren, wechseln und aktuell halten",
    "settings.vendorApi.label": "Adresse der Release-API",
    "settings.vendorApi.help": "Adoptium-kompatible API. Für einen Spiegel im eigenen Netz ändern.",
    "settings.defaultVersion.label": "Standard-JDK",
    "settings.installDir.label": "Installationsort",
    "settings.keepPatches.label": "Patchstände je Version behalten",
    "settings.envFile.label": "Datei für die Shell-Umgebung",
    "settings.envFile.help": "Setzt JAVA_HOME und PATH für neue Shells.",
    "checks.installed.label": "Installierte JDKs",
    "checks.latest.label": "Neue Versionen",
    "actions.install.label": "JDK installieren",
    "actions.install.params.version.label": "Version",
    "actions.install.params.makeDefault.label": "Danach als Standard setzen",
    "actions.update.label": "Aktualisieren und aufräumen",
    "actions.use.label": "Standard-JDK wechseln",
    "actions.use.params.version.label": "Version",
    "actions.remove.label": "JDK entfernen",
    "actions.remove.params.version.label": "Version",
    "actions.remove.confirm":
      "Das JDK wird gelöscht. Projekte, die es über JAVA_HOME nutzen, brauchen danach ein anderes.",
  },
  "node/de": {
    "plugin.description": "Node.js installieren, Versionen wechseln, globale Pakete mitnehmen",
    "settings.channel.label": "Kanal",
    "settings.distUrl.label": "Download-Adresse",
    "settings.globalPackages.label": "Globale Pakete",
    "actions.install.label": "Node.js installieren",
    "actions.update.label": "Aktualisieren",
  },
  "system/de": {
    "plugin.name": "System",
    "actions.selfUpdate.label": "DevBox aktualisieren",
  },
};

const baseTime = Date.parse("2026-03-01T12:00:00Z");

function iso(offsetMinutes: number): string {
  return new Date(baseTime + offsetMinutes * 60_000).toISOString();
}

const historyActions: {
  action: string;
  title: string;
  plugin: string;
  state: Session["state"];
  dryRun: boolean;
  changes: number;
  summary: string;
}[] = [
  {
    action: "java.update",
    title: "Update and clean up",
    plugin: "java",
    state: "success",
    dryRun: false,
    changes: 2,
    summary: "JDK 21.0.5 installed, 1 old patch version removed",
  },
  {
    action: "node.install",
    title: "Install Node.js",
    plugin: "node",
    state: "success",
    dryRun: false,
    changes: 1,
    summary: "Node.js 22.12.0 installed",
  },
  {
    action: "java.install",
    title: "Install JDK",
    plugin: "java",
    state: "failed",
    dryRun: false,
    changes: 0,
    summary: "Download failed: connection reset",
  },
  {
    action: "java.remove",
    title: "Remove JDK",
    plugin: "java",
    state: "success",
    dryRun: true,
    changes: 1,
    summary: "Would remove JDK 11.0.25",
  },
  {
    action: "system.selfUpdate",
    title: "Update DevBox",
    plugin: "system",
    state: "stopped",
    dryRun: false,
    changes: 0,
    summary: "Cancelled",
  },
  {
    action: "node.update",
    title: "Update",
    plugin: "node",
    state: "success",
    dryRun: false,
    changes: 1,
    summary: "Node.js 22.12.0 is current",
  },
];

const ID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/** A stable session ID (s_ plus ten Crockford base32 characters) from a counter. */
export function sessionIdFor(counter: number): string {
  let rest = counter;
  let out = "";
  for (let i = 0; i < 10; i++) {
    out = ID_ALPHABET[rest % 32] + out;
    rest = Math.floor(rest / 32);
  }
  return `s_${out}`;
}

/** Twelve finished sessions, newest first, so pagination has something to page. */
export function buildHistory(): Session[] {
  const sessions: Session[] = [];
  for (let i = 0; i < 12; i++) {
    const entry = historyActions[i % historyActions.length];
    if (!entry) {
      continue;
    }
    const start = -60 * (i + 1) * 5;
    sessions.push({
      id: sessionIdFor(1000 + i),
      action: entry.action,
      title: entry.title,
      state: entry.state,
      dryRun: entry.dryRun,
      plugin: entry.plugin,
      user: "dev",
      origin: i % 3 === 0 ? "cli" : "ui",
      exitCode: entry.state === "success" ? 0 : entry.state === "failed" ? 1 : null,
      changes: entry.changes,
      summary: entry.summary,
      startedAt: iso(start),
      endedAt: iso(start + 2),
    });
  }
  return sessions;
}
