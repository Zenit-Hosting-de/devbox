// An in-memory stand-in for the core: it answers the REST calls of the UI from the typed
// fixtures and simulates sessions (coloured output, step/progress/result events, a sudo
// prompt, cancel). The mock transport (interceptor and socket) is a thin layer over it.

import { Injectable } from "@angular/core";
import type {
  CheckSummary,
  CredentialList,
  Info,
  Manifest,
  Messages,
  PluginDetail,
  ProblemCode,
  ProblemError,
  Repository,
  RepositoryCreate,
  RepositoryPatch,
  RepositoryPlugin,
  ServerMessageOf,
  Session,
  SessionCreate,
  SessionEvent,
  SessionEventList,
  SessionList,
  SessionsQuery,
  SettingItem,
  SettingList,
  StatusReport,
  WsServerMessage,
} from "../api/types";
import { isLive } from "../api/types";
import { isRequired, validateField, type FormField } from "../schema-form/field";
import { base64ToBytes, bytesToBase64 } from "../ws/base64";
import {
  buildHistory,
  checksFixture,
  credentialsFixture,
  infoFixture,
  messagesFixture,
  pluginsFixture,
  repositoriesFixture,
  repositoryPlugin,
  sessionIdFor,
  settingSeeds,
} from "./fixtures";

/** A failed mock call; the interceptor turns it into an application/problem+json answer. */
export class MockProblem extends Error {
  constructor(
    readonly status: number,
    readonly code: ProblemCode,
    readonly detail: string,
    readonly errors: ProblemError[] = [],
  ) {
    super(detail);
  }
}

export interface MockListener {
  deliver(message: WsServerMessage): void;
}

interface Entry {
  seq: number;
  time: string;
  /** Raw bytes for output chunks. */
  bytes?: Uint8Array;
  event?: SessionEvent["event"];
}

interface Live {
  summary: Session;
  entries: Entry[];
  nextSeq: number;
  timers: ReturnType<typeof setTimeout>[];
  waiting: (() => void) | null;
  sudo: boolean;
}

const PLACEHOLDERS: Record<string, string> = {
  "${xdg:data}": "/home/dev/.local/share",
  "${xdg:config}": "/home/dev/.config",
};

function resolvePlaceholders(value: unknown): unknown {
  if (typeof value !== "string") {
    return value;
  }
  let out = value;
  for (const [token, replacement] of Object.entries(PLACEHOLDERS)) {
    out = out.replaceAll(token, replacement);
  }
  return out;
}

function ansi(code: string, text: string): string {
  return `\u001b[${code}m${text}\u001b[0m`;
}

@Injectable({ providedIn: "root" })
export class MockBackend {
  /** Simulated latency of REST answers in milliseconds. */
  latency = 150;
  /** Pause between the steps of a simulated session in milliseconds. */
  stepMs = 450;

  private readonly plugins: PluginDetail[] = structuredClone(pluginsFixture) as PluginDetail[];
  private readonly checks = new Map<string, CheckSummary>(
    structuredClone(checksFixture).map((c) => [c.id, c as CheckSummary]),
  );
  private readonly userValues = new Map<string, unknown>(
    settingSeeds
      .filter((s) => s.userValue !== undefined)
      .map((s) => [`${s.plugin}.${s.name}`, s.userValue]),
  );
  private readonly repositories: Repository[] = structuredClone(repositoriesFixture);
  /** Plugins that are switched off: ID -> layer (user, system, profile:<id>). */
  private readonly off = new Map<string, string>();
  private readonly sessions = new Map<string, Live>();
  private readonly listeners = new Set<MockListener>();
  private counter = 5000;

  constructor() {
    for (const summary of buildHistory()) {
      this.sessions.set(summary.id, {
        summary,
        entries: [
          {
            seq: 0,
            time: summary.startedAt ?? new Date().toISOString(),
            bytes: new TextEncoder().encode(
              `${ansi("1;34", `==> ${summary.title ?? summary.action}`)}\r\n${summary.summary ?? ""}\r\n`,
            ),
          },
        ],
        nextSeq: 1,
        timers: [],
        waiting: null,
        sudo: false,
      });
    }
  }

  // ---- listeners (sockets) -------------------------------------------------

  addListener(listener: MockListener): void {
    this.listeners.add(listener);
  }

  removeListener(listener: MockListener): void {
    this.listeners.delete(listener);
  }

  private broadcast(message: WsServerMessage): void {
    for (const listener of this.listeners) {
      listener.deliver(message);
    }
  }

  /** The mock core announces that it restarts with a new version (WebSocket message `restart`). */
  announceRestart(): void {
    this.broadcast({ type: "restart" });
  }

  /** Output and events of a session from `fromSeq` on, for a resubscribing socket. */
  replay(id: string, fromSeq: number): WsServerMessage[] {
    const live = this.sessions.get(id);
    if (!live) {
      return [];
    }
    return live.entries.filter((e) => e.seq >= fromSeq).map((e) => this.toMessage(id, e));
  }

  private toMessage(session: string, entry: Entry): WsServerMessage {
    if (entry.bytes) {
      return { type: "output", session, seq: entry.seq, data: bytesToBase64(entry.bytes) };
    }
    return {
      type: "event",
      session,
      seq: entry.seq,
      time: entry.time,
      event: (entry.event ?? {}) as Record<string, unknown>,
    } as WsServerMessage;
  }

  // ---- reads ----------------------------------------------------------------

  info(): Info {
    return structuredClone(infoFixture) as Info;
  }

  pluginList(manifest: boolean): PluginDetail[] {
    return this.plugins.map((p) => {
      const copy = structuredClone(p);
      const layer = this.off.get(p.id);
      if (layer) {
        copy.state = "disabled";
        copy.reasons = [{ code: "disabled", detail: layer }];
      }
      if (!manifest) {
        delete copy.manifest;
        delete copy.resolvedDefaults;
      }
      return copy;
    });
  }

  messages(plugin: string, lang: string): Messages {
    const found = messagesFixture[`${plugin}/${lang}`];
    if (!found) {
      throw new MockProblem(404, "not-found", `No translation ${lang} for ${plugin}.`);
    }
    return structuredClone(found);
  }

  statusReport(): StatusReport {
    return {
      host: this.info().host,
      summary: this.summary(),
      checks: [...this.checks.values()].map((c) => structuredClone(c)),
      running: [],
    };
  }

  private summary(): StatusReport["summary"] {
    const counts = { ok: 0, warn: 0, error: 0, missing: 0 };
    for (const check of this.checks.values()) {
      counts[check.status]++;
    }
    return counts;
  }

  check(id: string): CheckSummary {
    const found = this.checks.get(id);
    if (!found) {
      throw new MockProblem(404, "not-found", `No result for ${id}.`);
    }
    return structuredClone(found);
  }

  credentials(): CredentialList {
    return { instances: structuredClone(credentialsFixture) };
  }

  // ---- checks ---------------------------------------------------------------

  refreshChecks(ids: readonly string[] | undefined): void {
    const targets = ids && ids.length > 0 ? [...ids] : [...this.checks.keys()];
    const errors: ProblemError[] = [];
    targets.forEach((id, index) => {
      if (!this.checks.has(id)) {
        errors.push({ pointer: `/ids/${index}`, code: "unknown", message: `Unknown check ${id}` });
      }
    });
    if (errors.length > 0) {
      throw new MockProblem(422, "validation", "Unknown checks.", errors);
    }
    targets.forEach((id, index) => {
      setTimeout(
        () => {
          const current = this.checks.get(id);
          if (!current) {
            return;
          }
          const next: CheckSummary = { ...current, checkedAt: new Date().toISOString() };
          this.checks.set(id, next);
          this.broadcast({
            type: "check",
            id,
            result: next as unknown as ServerMessageOf<"check">["result"],
          });
        },
        this.stepMs * (index + 1),
      );
    });
  }

  // ---- settings -------------------------------------------------------------

  private manifestFields(): { plugin: PluginDetail; name: string; field: FormField }[] {
    const out: { plugin: PluginDetail; name: string; field: FormField }[] = [];
    for (const plugin of this.plugins) {
      const settings = (plugin.manifest as Manifest | undefined)?.settings ?? {};
      for (const [name, field] of Object.entries(settings)) {
        out.push({ plugin, name, field });
      }
    }
    return out.sort((a, b) => `${a.plugin.id}.${a.name}`.localeCompare(`${b.plugin.id}.${b.name}`));
  }

  settingList(): SettingList {
    const settings: SettingItem[] = this.manifestFields().map(({ plugin, name, field }) => {
      const key = `${plugin.id}.${name}`;
      const seed = settingSeeds.find((s) => s.plugin === plugin.id && s.name === name);
      const item: SettingItem = { key, plugin: plugin.id, field, locked: false };
      if (seed?.lockedBy) {
        item.locked = true;
        item.lockedBy = seed.lockedBy;
        item.value = seed.lockedValue;
        item.origin = seed.lockedBy;
      } else if (this.userValues.has(key)) {
        item.value = this.userValues.get(key);
        item.origin = "user";
      } else if (field.default !== undefined) {
        item.value = resolvePlaceholders(field.default);
        item.origin = "plugin";
      }
      return item;
    });
    return { settings: structuredClone(settings) };
  }

  patchSettings(patch: Readonly<Record<string, unknown>>): void {
    const fields = new Map(this.manifestFields().map((f) => [`${f.plugin.id}.${f.name}`, f]));
    const errors: ProblemError[] = [];
    const lockedKeys: string[] = [];
    for (const [key, value] of Object.entries(patch)) {
      const known = fields.get(key);
      if (!known) {
        throw new MockProblem(404, "not-found", `Unknown setting ${key}.`);
      }
      if (
        settingSeeds.some(
          (s) => s.plugin === known.plugin.id && s.name === known.name && s.lockedBy,
        )
      ) {
        lockedKeys.push(key);
        errors.push({ pointer: `/${key}`, code: "locked", message: "locked by a profile" });
        continue;
      }
      if (value === null) {
        continue;
      }
      const error = validateField(known.field, value, false);
      if (error) {
        errors.push({
          pointer: `/${key}`,
          code: error.code,
          message: `Invalid value: ${error.code}`,
        });
      }
    }
    if (lockedKeys.length > 0 && errors.length === lockedKeys.length) {
      throw new MockProblem(
        409,
        "setting-locked",
        `${lockedKeys[0]} is locked by a profile.`,
        errors,
      );
    }
    if (errors.length > 0) {
      throw new MockProblem(422, "validation", "Some values are invalid.", errors);
    }
    for (const [key, value] of Object.entries(patch)) {
      if (value === null) {
        this.userValues.delete(key);
      } else {
        this.userValues.set(key, value);
      }
    }
    const keys = Object.keys(patch);
    setTimeout(() => {
      this.broadcast({ type: "settings", keys });
    }, 50);
  }

  // ---- plugins on/off and repositories ---------------------------------------

  /** Marks a plugin as switched off by a layer other than the user, e.g. `profile:example-team`. */
  switchOffBy(id: string, layer: string): void {
    this.off.set(id, layer);
  }

  patchPlugin(id: string, enabled: boolean): PluginDetail {
    const plugin = this.plugins.find((p) => p.id === id);
    if (!plugin) {
      throw new MockProblem(404, "not-found", `No plugin ${id}.`);
    }
    if (!enabled && plugin.category === "core") {
      throw new MockProblem(409, "conflict", "core plugins cannot be switched off");
    }
    const layer = this.off.get(id);
    if (enabled && layer && layer !== "user") {
      throw new MockProblem(
        409,
        "conflict",
        `switched off by ${layer}; the user cannot switch it on`,
      );
    }
    if (enabled) {
      this.off.delete(id);
    } else {
      this.off.set(id, "user");
    }
    this.announcePlugins([id], []);
    const found = this.pluginList(false).find((p) => p.id === id);
    if (!found) {
      throw new MockProblem(404, "not-found", `No plugin ${id}.`);
    }
    return found;
  }

  repositoryList(): Repository[] {
    return structuredClone(this.repositories).sort((a, b) => a.name.localeCompare(b.name));
  }

  /** The revision every mock sync finds. */
  static readonly remoteRevision = "9a8b7c6d5e4f";

  private remotePlugins(): RepositoryPlugin[] {
    return [
      repositoryPlugin("demo", "1.0.0", {
        description: "Example plugin from the mock source.",
        permissions: [
          { kind: "network", value: "api.example.com", new: false },
          { kind: "sudo", value: "true", new: false },
        ],
        requiresCommands: ["curl"],
      }),
    ];
  }

  private findRepository(name: string): Repository {
    const found = this.repositories.find((r) => r.name === name);
    if (!found) {
      throw new MockProblem(404, "not-found", `No repository ${name}.`);
    }
    return found;
  }

  createRepository(body: RepositoryCreate): Repository {
    const errors: ProblemError[] = [];
    if (!/^[a-z][a-z0-9-]{1,31}$/.test(body.name)) {
      errors.push({ pointer: "/name", code: "pattern", message: "invalid name" });
    }
    if (!/^(git\+https|git\+ssh|git\+file|https|file):\/\/.+/.test(body.source)) {
      errors.push({ pointer: "/source", code: "pattern", message: "unsupported source" });
    }
    if (body.trust === "pinned" && !/^[0-9a-f]{40}$/.test(body.ref ?? "")) {
      errors.push({ pointer: "/ref", code: "pinned-ref", message: "pinned needs a full commit" });
    }
    if (errors.length > 0) {
      throw new MockProblem(422, "validation", "The repository is invalid.", errors);
    }
    if (this.repositories.some((r) => r.name === body.name)) {
      throw new MockProblem(409, "conflict", `A repository named ${body.name} exists.`);
    }
    const entry: Repository = {
      name: body.name,
      kind: "plugins",
      source: body.source,
      ...(body.ref ? { ref: body.ref } : {}),
      ...(body.path ? { path: body.path } : {}),
      trust: body.trust ?? "prompt",
      enabled: true,
      origin: "user",
      locked: false,
      state: "new",
    };
    this.repositories.push(entry);
    this.runSync(entry);
    this.announcePlugins([], [entry.name]);
    return structuredClone(entry);
  }

  /** Changes an entry of the user configuration and syncs it; null removes ref and path. */
  patchRepository(name: string, body: RepositoryPatch): Repository {
    const entry = this.findRepository(name);
    if (entry.origin !== "user" || entry.locked) {
      throw new MockProblem(
        409,
        "conflict",
        `The repository comes from ${entry.origin} and can only be changed there.`,
      );
    }
    const errors: ProblemError[] = [];
    const source = body.source ?? entry.source ?? "";
    if (!/^(git\+https|git\+ssh|git\+file|https|file):\/\/.+/.test(source)) {
      errors.push({ pointer: "/source", code: "pattern", message: "unsupported source" });
    }
    const ref = body.ref === undefined ? entry.ref : (body.ref ?? "");
    const trust = body.trust ?? entry.trust;
    if (trust === "pinned" && !/^[0-9a-f]{40}$/.test(ref ?? "")) {
      errors.push({ pointer: "/ref", code: "pinned-ref", message: "pinned needs a full commit" });
    }
    if (errors.length > 0) {
      throw new MockProblem(422, "validation", "The repository is invalid.", errors);
    }
    const path = body.path === undefined ? entry.path : (body.path ?? "");
    entry.source = source;
    entry.trust = trust;
    delete entry.ref;
    delete entry.path;
    if (ref) {
      entry.ref = ref;
    }
    if (path) {
      entry.path = path;
    }
    this.runSync(entry);
    this.announcePlugins([], [name]);
    return structuredClone(entry);
  }

  syncRepository(name: string): Repository {
    const entry = this.findRepository(name);
    this.runSync(entry);
    this.announcePlugins([], [name]);
    return structuredClone(entry);
  }

  /**
   * A source whose address contains "fail" cannot be fetched. One that contains "subfolder"
   * keeps its plugins in `plugins/`: with another path it holds none and names that folder.
   * Every other one has one revision.
   */
  private runSync(entry: Repository): void {
    const now = new Date().toISOString();
    if (entry.source?.includes("fail")) {
      entry.state = "error";
      entry.lastError = "git clone failed: repository not found";
      return;
    }
    delete entry.lastError;
    const revision = MockBackend.remoteRevision;
    const path = entry.path ?? "";
    const isCurrent = (r?: { revision: string; path?: string }): boolean =>
      r?.revision === revision && (r.path ?? "") === path;
    if (isCurrent(entry.active) || isCurrent(entry.pending)) {
      if (isCurrent(entry.active)) {
        delete entry.pending;
        entry.state = "active";
      }
      entry.lastSync = { at: now, changed: false };
      return;
    }
    const misplaced = entry.source?.includes("subfolder") === true && path !== "plugins";
    const next = misplaced ? [] : this.remotePlugins();
    const suggestedPaths = misplaced ? { suggestedPaths: ["plugins"] } : {};
    if (entry.trust === "pinned") {
      entry.active = {
        revision,
        syncedAt: now,
        plugins: next,
        ...suggestedPaths,
        ...(entry.path ? { path: entry.path } : {}),
      };
      delete entry.pending;
      entry.state = "active";
      entry.lastSync = { at: now, changed: true };
      this.activate(entry);
      return;
    }
    const known = new Map((entry.active?.plugins ?? []).map((p) => [p.id, p]));
    const plugins: RepositoryPlugin[] = next.map((p) => {
      const before = known.get(p.id);
      known.delete(p.id);
      if (!before) {
        return { ...p, change: "added" };
      }
      return before.digest === p.digest
        ? { ...p, change: "unchanged" }
        : {
            ...p,
            change: "changed",
            ...(before.version ? { previousVersion: before.version } : {}),
            permissions: p.permissions.map((perm) => ({
              ...perm,
              new: !before.permissions.some((b) => b.kind === perm.kind && b.value === perm.value),
            })),
          };
    });
    for (const gone of known.values()) {
      plugins.push({ ...gone, change: "removed" });
    }
    entry.pending = {
      revision,
      fetchedAt: now,
      plugins: plugins.map((p) =>
        p.change === "added"
          ? { ...p, permissions: p.permissions.map((perm) => ({ ...perm, new: true })) }
          : p,
      ),
      ...suggestedPaths,
      ...(entry.path ? { path: entry.path } : {}),
    };
    entry.state = "pending";
    entry.lastSync = { at: now, changed: true };
  }

  approveRepository(name: string, revision: string): Repository {
    const entry = this.findRepository(name);
    if (!entry.pending) {
      throw new MockProblem(409, "conflict", "No revision waits for approval.");
    }
    if (entry.pending.revision !== revision) {
      throw new MockProblem(
        409,
        "conflict",
        `The pending revision is ${entry.pending.revision}, not ${revision}.`,
      );
    }
    const pending = entry.pending;
    entry.active = {
      revision: pending.revision,
      syncedAt: pending.fetchedAt,
      plugins: pending.plugins
        .filter((p) => p.change !== "removed")
        .map((p) => {
          const active = {
            ...p,
            permissions: p.permissions.map((perm) => ({ ...perm, new: false })),
          };
          // The diff fields exist in a pending revision only.
          delete active.change;
          delete active.previousVersion;
          return active;
        }),
      ...(pending.path ? { path: pending.path } : {}),
    };
    delete entry.pending;
    entry.state = "active";
    this.activate(entry);
    this.announcePlugins(
      entry.active.plugins.map((p) => p.id),
      [name],
    );
    return structuredClone(entry);
  }

  discardRepository(name: string): Repository {
    const entry = this.findRepository(name);
    delete entry.pending;
    entry.state = entry.active ? "active" : "new";
    this.announcePlugins([], [name]);
    return structuredClone(entry);
  }

  deleteRepository(name: string): void {
    const entry = this.findRepository(name);
    if (entry.origin !== "user") {
      throw new MockProblem(
        409,
        "conflict",
        `${name} comes from the ${entry.origin} configuration and cannot be removed here.`,
      );
    }
    this.repositories.splice(this.repositories.indexOf(entry), 1);
    this.dropPlugins(name);
    this.announcePlugins([], [name]);
  }

  private dropPlugins(repository: string): void {
    for (let i = this.plugins.length - 1; i >= 0; i--) {
      const origin = this.plugins[i]?.origin;
      if (origin?.kind === "repository" && origin.repository === repository) {
        this.plugins.splice(i, 1);
      }
    }
  }

  /** Makes the plugins of the active revision appear in the plugin list. */
  private activate(entry: Repository): void {
    this.dropPlugins(entry.name);
    for (const p of entry.active?.plugins ?? []) {
      if (this.plugins.some((other) => other.id === p.id)) {
        continue;
      }
      this.plugins.push({
        id: p.id,
        ...(p.name ? { name: p.name } : {}),
        ...(p.version ? { version: p.version } : {}),
        category: "tool",
        state: "ready",
        reasons: [],
        path: `/home/dev/.local/share/devbox/repositories/${entry.name}/${entry.active?.revision ?? ""}/${p.id}`,
        origin: {
          kind: "repository",
          repository: entry.name,
          revision: entry.active?.revision ?? "",
        },
        manifest: {
          id: p.id,
          version: p.version ?? "0.0.0",
          name: p.name ?? p.id,
          ...(p.description ? { description: p.description } : {}),
          category: "tool",
        },
      });
    }
    this.plugins.sort((a, b) => a.id.localeCompare(b.id));
  }

  private announcePlugins(ids: string[], repositories: string[]): void {
    setTimeout(() => {
      this.broadcast({ type: "plugins", ids, repositories });
    }, 50);
  }

  // ---- sessions -------------------------------------------------------------

  listSessions(query: SessionsQuery): SessionList {
    const limit = query.limit ?? 50;
    const all = [...this.sessions.values()]
      .map((l) => this.snapshot(l))
      .filter((s) => (query.state ? s.state === query.state : true))
      .filter((s) => (query.plugin ? s.plugin === query.plugin : true))
      .sort((a, b) => (b.startedAt ?? "").localeCompare(a.startedAt ?? ""));
    const offset = query.cursor ? Number(query.cursor) || 0 : 0;
    const page = all.slice(offset, offset + limit);
    const more = offset + limit < all.length;
    return { sessions: page, ...(more ? { next: String(offset + limit) } : {}) };
  }

  private snapshot(live: Live): Session {
    const copy = structuredClone(live.summary);
    if (isLive(copy.state)) {
      copy.nextSeq = live.nextSeq;
    }
    return copy;
  }

  private find(id: string): Live {
    const live = this.sessions.get(id);
    if (!live) {
      throw new MockProblem(404, "not-found", `Unknown session ${id}.`);
    }
    return live;
  }

  session(id: string): Session {
    return this.snapshot(this.find(id));
  }

  sessionEvents(id: string): SessionEventList {
    const live = this.find(id);
    const events = live.entries
      .filter((e) => e.event !== undefined)
      .map((e) => ({ seq: e.seq, time: e.time, event: e.event as SessionEvent["event"] }));
    return { events };
  }

  sessionLog(id: string): { data: Uint8Array; nextSeq: number } {
    const live = this.find(id);
    const chunks = live.entries.flatMap((e) => (e.bytes ? [e.bytes] : []));
    const data = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
    let offset = 0;
    for (const chunk of chunks) {
      data.set(chunk, offset);
      offset += chunk.length;
    }
    return { data, nextSeq: live.nextSeq };
  }

  createSession(body: SessionCreate): Session {
    const [pluginId, actionName] = body.action.split(".");
    const plugin = this.plugins.find((p) => p.id === pluginId);
    const manifest = plugin?.manifest as Manifest | undefined;
    const action = actionName ? manifest?.actions?.[actionName] : undefined;
    if (!plugin || !action || !actionName) {
      throw new MockProblem(404, "not-found", `Unknown action ${body.action}.`);
    }
    if (plugin.state !== "ready") {
      throw new MockProblem(409, "plugin-not-ready", `Plugin ${plugin.id} is ${plugin.state}.`);
    }
    const params = (body.params ?? {}) as Record<string, unknown>;
    const errors: ProblemError[] = [];
    const defs = action.params ?? {};
    for (const name of Object.keys(params)) {
      if (!(name in defs)) {
        errors.push({ pointer: `/params/${name}`, code: "unknown", message: "unknown parameter" });
      }
    }
    for (const [name, field] of Object.entries(defs)) {
      const error = validateField(field, params[name], isRequired(field));
      if (error) {
        errors.push({ pointer: `/params/${name}`, code: error.code, message: error.code });
      }
    }
    if (action.confirm && !body.dryRun && body.yes !== true) {
      errors.push({ pointer: "/yes", code: "confirm", message: "confirmation required" });
    }
    if (errors.length > 0) {
      throw new MockProblem(422, "validation", "The request is invalid.", errors);
    }
    for (const live of this.sessions.values()) {
      if (live.summary.action === body.action && isLive(live.summary.state)) {
        throw new MockProblem(409, "conflict", `${body.action} is already running.`);
      }
    }
    this.counter++;
    const now = new Date().toISOString();
    const summary: Session = {
      id: sessionIdFor(this.counter),
      action: body.action,
      title: action.label,
      state: "queued",
      dryRun: body.dryRun === true,
      plugin: plugin.id,
      user: "dev",
      origin: "ui",
      exitCode: null,
      changes: 0,
      startedAt: now,
      progress: { percent: null },
    };
    const live: Live = {
      summary,
      entries: [],
      nextSeq: 0,
      timers: [],
      waiting: null,
      sudo: action.sudo === true,
    };
    this.sessions.set(summary.id, live);
    this.schedule(live, this.stepMs / 3, () => {
      this.run(live, action.label);
    });
    return this.snapshot(live);
  }

  cancelSession(id: string): Session {
    const live = this.find(id);
    if (!isLive(live.summary.state)) {
      throw new MockProblem(409, "conflict", `Session ${id} is already finished.`);
    }
    const asRequested = this.snapshot(live);
    for (const timer of live.timers) {
      clearTimeout(timer);
    }
    live.timers = [];
    live.waiting = null;
    this.schedule(live, 100, () => {
      this.output(live, `\r\n${ansi("33", "^C Cancelled")}\r\n`);
      this.finish(live, "stopped", "Cancelled", null);
    });
    return asRequested;
  }

  /** Keyboard input of a terminal: echoed, and it continues a sudo prompt. */
  input(id: string, base64: string): void {
    const live = this.sessions.get(id);
    if (!live || !isLive(live.summary.state)) {
      return;
    }
    const text = new TextDecoder().decode(base64ToBytes(base64));
    if (live.waiting) {
      const echo = text.replace(/[^\r\n]/g, "*");
      this.output(live, echo);
      if (text.includes("\r") || text.includes("\n")) {
        const resume = live.waiting;
        live.waiting = null;
        this.output(live, "\r\n");
        resume();
      }
      return;
    }
    this.output(live, text.replaceAll("\r", "\r\n"));
  }

  // ---- simulation -----------------------------------------------------------

  private schedule(live: Live, delay: number, fn: () => void): void {
    live.timers.push(setTimeout(fn, delay));
  }

  private emitSession(live: Live): void {
    this.broadcast({ type: "session", session: this.snapshot(live) });
  }

  private output(live: Live, text: string): void {
    const entry: Entry = {
      seq: live.nextSeq++,
      time: new Date().toISOString(),
      bytes: new TextEncoder().encode(text),
    };
    live.entries.push(entry);
    this.broadcast(this.toMessage(live.summary.id, entry));
  }

  private event(live: Live, event: SessionEvent["event"]): void {
    const entry: Entry = { seq: live.nextSeq++, time: new Date().toISOString(), event };
    live.entries.push(entry);
    this.broadcast(this.toMessage(live.summary.id, entry));
  }

  private progress(live: Live, percent: number | null, text: string): void {
    live.summary.progress = { percent, text };
    this.event(live, { type: "progress", percent, text });
  }

  private run(live: Live, label: string): void {
    const dry = live.summary.dryRun;
    live.summary.state = "running";
    this.emitSession(live);
    const prefix = dry ? `${ansi("36", "(dry run)")} ` : "";
    this.output(live, `${ansi("1;34", `==> ${label}`)}\r\n`);
    this.event(live, { type: "step", id: "prepare", label: "Prepare", state: "running" });
    this.output(live, `${prefix}resolving sources from example.com\r\n`);
    this.progress(live, 10, "Resolving");

    const afterPrompt = (): void => {
      this.event(live, { type: "step", id: "download", label: "Download", state: "running" });
      this.output(live, `${prefix}downloading archive ${ansi("2", "(48 MiB)")}\r\n`);
      this.progress(live, 45, "Downloading");
      this.schedule(live, this.stepMs, () => {
        this.event(live, { type: "step", id: "download", label: "Download", state: "done" });
        this.event(live, { type: "step", id: "install", label: "Install", state: "running" });
        this.output(live, `${prefix}unpacking ${ansi("32", "ok")}\r\n`);
        this.progress(live, 80, "Installing");
        this.schedule(live, this.stepMs, () => {
          this.event(live, { type: "step", id: "install", label: "Install", state: "done" });
          this.event(live, {
            type: "change",
            kind: "file",
            op: dry ? "update" : "create",
            target: "/home/dev/.local/share/example",
            summary: dry ? "would create" : "created",
          });
          live.summary.changes = 1;
          this.progress(live, 100, "Done");
          const summary = dry ? "Dry run finished, 1 change planned" : "Finished, 1 change applied";
          this.output(live, `${ansi("1;32", "done")} ${summary}\r\n`);
          this.event(live, { type: "result", summary });
          this.finish(live, "success", summary, 0);
        });
      });
    };

    this.schedule(live, this.stepMs, () => {
      this.event(live, { type: "step", id: "prepare", label: "Prepare", state: "done" });
      if (live.sudo && !dry) {
        this.output(live, "[sudo] password for dev: ");
        live.waiting = afterPrompt;
        return;
      }
      afterPrompt();
    });
  }

  private finish(
    live: Live,
    state: Session["state"],
    summary: string,
    exitCode: number | null,
  ): void {
    live.summary.state = state;
    live.summary.summary = summary;
    live.summary.exitCode = exitCode;
    live.summary.endedAt = new Date().toISOString();
    delete live.summary.progress;
    this.emitSession(live);
  }
}
