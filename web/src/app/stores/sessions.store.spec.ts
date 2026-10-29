import { TestBed } from "@angular/core/testing";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import type { Session, WsServerMessage } from "../api/types";
import { I18n } from "../i18n/i18n";
import { bytesToBase64 } from "../ws/base64";
import { SOCKET_FACTORY, browserSocketFactory, type SocketLike } from "../ws/socket";
import { setUpApp } from "../testing/test-app";
import { ChecksStore } from "./checks.store";
import { CredentialsStore } from "./credentials.store";
import { PluginsStore } from "./plugins.store";
import { BACKOFF_MAX_MS, BACKOFF_START_MS, SessionsStore } from "./sessions.store";
import { SettingsStore } from "./settings.store";

class FakeSocket implements SocketLike {
  onopen: (() => void) | null = null;
  onmessage: ((data: string) => void) | null = null;
  onclose: (() => void) | null = null;
  readonly sent: Record<string, unknown>[] = [];
  closed = false;

  constructor(readonly url: string) {}

  send(data: string): void {
    this.sent.push(JSON.parse(data) as Record<string, unknown>);
  }
  close(): void {
    this.closed = true;
    this.onclose?.();
  }
  open(): void {
    this.onopen?.();
  }
  receive(message: unknown): void {
    this.onmessage?.(JSON.stringify(message));
  }
  /** The connection drops without the page closing it. */
  drop(): void {
    this.onclose?.();
  }
}

const SESSION: Session = {
  id: "s_0123456789",
  action: "java.update",
  title: "Update",
  state: "running",
  dryRun: false,
};

function output(seq: number, text: string): WsServerMessage {
  return {
    type: "output",
    session: SESSION.id,
    seq,
    data: bytesToBase64(new TextEncoder().encode(text)),
  };
}

function event(seq: number, payload: Record<string, unknown>): WsServerMessage {
  return {
    type: "event",
    session: SESSION.id,
    seq,
    time: "2026-03-01T12:00:00Z",
    event: payload,
  } as unknown as WsServerMessage;
}

describe("SessionsStore", () => {
  let sockets: FakeSocket[];

  function use(): SessionsStore {
    sockets = [];
    setUpApp([
      {
        provide: SOCKET_FACTORY,
        useValue: (url: string): SocketLike => {
          const socket = new FakeSocket(url);
          sockets.push(socket);
          return socket;
        },
      },
    ]);
    return TestBed.inject(SessionsStore);
  }

  afterEach(() => {
    vi.useRealTimers();
  });

  it("owns exactly one WebSocket instance, however often it is asked to connect", () => {
    class CountingWebSocket {
      static instances: CountingWebSocket[] = [];
      onopen: (() => void) | null = null;
      onmessage: ((event: unknown) => void) | null = null;
      onclose: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor(readonly url: string) {
        CountingWebSocket.instances.push(this);
      }
      send(): void {
        // Not needed.
      }
      close(): void {
        // Not needed.
      }
    }
    const realWebSocket = globalThis.WebSocket;
    globalThis.WebSocket = CountingWebSocket as unknown as typeof WebSocket;
    try {
      // The real browser socket factory, with the stubbed constructor behind it.
      setUpApp([{ provide: SOCKET_FACTORY, useValue: browserSocketFactory }]);
      const store = TestBed.inject(SessionsStore);
      // Every store of the application is created; none opens a socket of its own.
      TestBed.inject(PluginsStore);
      TestBed.inject(ChecksStore);
      TestBed.inject(SettingsStore);
      TestBed.inject(CredentialsStore);
      store.connect();
      store.connect();
      store.connect();
      expect(CountingWebSocket.instances).toHaveLength(1);
      expect(CountingWebSocket.instances[0]?.url).toMatch(/^wss?:\/\/[^/]+\/api\/v1\/ws$/);
    } finally {
      globalThis.WebSocket = realWebSocket;
    }
  });

  it("subscribes to checks, sessions and settings once the socket is open", () => {
    const store = use();
    store.connect();
    expect(store.status()).toBe("connecting");
    sockets[0]?.open();
    expect(store.status()).toBe("open");
    expect(sockets[0]?.sent).toEqual([
      { type: "subscribe", topics: ["checks", "sessions", "settings", "plugins"] },
    ]);
  });

  it("answers ping with pong", () => {
    const store = use();
    store.connect();
    sockets[0]?.open();
    sockets[0]?.receive({ type: "ping" });
    expect(sockets[0]?.sent.at(-1)).toEqual({ type: "pong" });
  });

  it("reconnects with a backoff of 1 s doubling up to 30 s and shows offline meanwhile", () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const store = use();
    store.connect();
    sockets[0]?.open();
    sockets[0]?.drop();
    expect(store.status()).toBe("offline");

    vi.advanceTimersByTime(BACKOFF_START_MS - 1);
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(2);

    sockets[1]?.drop(); // never opened: the next delay doubles
    vi.advanceTimersByTime(BACKOFF_START_MS * 2 - 1);
    expect(sockets).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(3);

    let delay = BACKOFF_START_MS * 4;
    for (let i = 3; i < 10; i++) {
      sockets[i - 1]?.drop();
      const expected = Math.min(delay, BACKOFF_MAX_MS);
      vi.advanceTimersByTime(expected);
      expect(sockets).toHaveLength(i + 1);
      delay *= 2;
    }
    expect(Math.min(delay, BACKOFF_MAX_MS)).toBe(BACKOFF_MAX_MS);

    sockets.at(-1)?.open();
    expect(store.status()).toBe("open");
  });

  it("resubscribes after a reconnect with fromSeq of the last seen seq plus one and reloads over REST", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const store = use();
    const api = TestBed.inject(ApiClient);
    const checks = vi.spyOn(api, "checks");
    const settings = vi.spyOn(api, "settings");
    const listSessions = vi.spyOn(api, "sessions");
    const plugins = vi.spyOn(api, "plugins");
    // The stores register their reload hooks when they are created.
    TestBed.inject(PluginsStore);
    TestBed.inject(ChecksStore);
    TestBed.inject(SettingsStore);
    TestBed.inject(CredentialsStore);

    store.connect();
    sockets[0]?.open();
    const channel = store.openNew(SESSION);
    expect(sockets[0]?.sent.at(-1)).toEqual({
      type: "subscribe",
      topics: [`session:${SESSION.id}`],
      fromSeq: 0,
    });
    sockets[0]?.receive(output(0, "a"));
    sockets[0]?.receive(event(1, { type: "step", id: "x", label: "X", state: "running" }));
    sockets[0]?.receive(output(2, "b"));
    expect(channel.expectedSeq).toBe(3);

    sockets[0]?.drop();
    vi.advanceTimersByTime(BACKOFF_START_MS);
    sockets[1]?.open();

    expect(sockets[1]?.sent).toEqual([
      { type: "subscribe", topics: ["checks", "sessions", "settings", "plugins"] },
      { type: "subscribe", topics: [`session:${SESSION.id}`], fromSeq: 3 },
    ]);
    await vi.advanceTimersByTimeAsync(0);
    expect(checks).toHaveBeenCalled();
    expect(settings).toHaveBeenCalled();
    expect(listSessions).toHaveBeenCalled();
    expect(plugins).toHaveBeenCalled();
  });

  it("drops messages below the expected seq", () => {
    const store = use();
    store.connect();
    sockets[0]?.open();
    const channel = store.openNew(SESSION);
    const written: string[] = [];
    channel.attach((bytes) => written.push(new TextDecoder().decode(bytes)));

    sockets[0]?.receive(output(0, "A"));
    sockets[0]?.receive(output(0, "A"));
    sockets[0]?.receive(output(1, "B"));
    sockets[0]?.receive(output(1, "B"));
    sockets[0]?.receive(event(2, { type: "step", id: "s", label: "S", state: "done" }));
    sockets[0]?.receive(event(2, { type: "step", id: "s", label: "S", state: "done" }));
    sockets[0]?.receive(output(1, "late"));
    sockets[0]?.receive(output(3, "C"));

    expect(written).toEqual(["A", "B", "C"]);
    expect(channel.events()).toHaveLength(1);
    expect(channel.expectedSeq).toBe(4);
  });

  it("opens a running session from the log and subscribes from the sequence the log ends at", async () => {
    const store = use();
    const api = TestBed.inject(ApiClient);
    vi.spyOn(api, "sessionLog").mockResolvedValue({
      data: new TextEncoder().encode("history"),
      nextSeq: 7,
    });
    vi.spyOn(api, "sessionEvents").mockResolvedValue({
      events: [
        {
          seq: 3,
          time: "2026-03-01T12:00:00Z",
          event: { type: "step", id: "s", label: "S", state: "running" },
        },
      ],
    });
    store.connect();
    sockets[0]?.open();
    const channel = await store.openExisting(SESSION);
    const written: string[] = [];
    channel.attach((bytes) => written.push(new TextDecoder().decode(bytes)));
    expect(written).toEqual(["history"]);
    expect(channel.events()).toHaveLength(1);
    expect(sockets[0]?.sent.at(-1)).toEqual({
      type: "subscribe",
      topics: [`session:${SESSION.id}`],
      fromSeq: 7,
    });
    // The replay may repeat what the log already holds.
    sockets[0]?.receive(output(6, "old"));
    sockets[0]?.receive(output(7, "new"));
    expect(written).toEqual(["history", "new"]);
  });

  it("remembers the summary of a session that is opened from the history", async () => {
    const store = use();
    const api = TestBed.inject(ApiClient);
    vi.spyOn(api, "sessionLog").mockResolvedValue({ data: new Uint8Array(), nextSeq: 0 });
    vi.spyOn(api, "sessionEvents").mockResolvedValue({ events: [] });
    expect(store.sessions().has(SESSION.id)).toBe(false);
    await store.openExisting(SESSION);
    expect(store.sessions().get(SESSION.id)).toEqual(SESSION);
    expect(store.live().map((s) => s.id)).toEqual([SESSION.id]);
  });

  it("resets the terminal and reloads the log on a gap", async () => {
    const store = use();
    const api = TestBed.inject(ApiClient);
    const log = vi.spyOn(api, "sessionLog");
    log.mockResolvedValueOnce({ data: new Uint8Array(), nextSeq: 0 });
    vi.spyOn(api, "sessionEvents").mockResolvedValue({ events: [] });
    store.connect();
    sockets[0]?.open();
    const channel = await store.openExisting(SESSION);
    const actions: string[] = [];
    channel.attach(
      (bytes) => actions.push(`write:${new TextDecoder().decode(bytes)}`),
      () => actions.push("reset"),
    );
    sockets[0]?.receive(output(0, "before"));

    log.mockResolvedValueOnce({ data: new TextEncoder().encode("LOG"), nextSeq: 10 });
    sockets[0]?.receive({ type: "gap", session: SESSION.id, fromSeq: 0, firstSeq: 8 });
    // Streaming already continues while the log loads; those messages wait.
    sockets[0]?.receive(output(8, "stale"));
    sockets[0]?.receive(output(10, "live"));
    await vi.waitFor(() => {
      expect(channel.loading()).toBe(false);
    });

    expect(actions).toEqual(["write:before", "reset", "write:LOG", "write:live"]);
    expect(log).toHaveBeenCalledTimes(2);
  });

  it("routes check messages to the checks store and settings messages to the settings store", async () => {
    const store = use();
    const checks = TestBed.inject(ChecksStore);
    const settings = TestBed.inject(SettingsStore);
    const reload = vi.spyOn(TestBed.inject(ApiClient), "settings");
    store.connect();
    sockets[0]?.open();

    sockets[0]?.receive({
      type: "check",
      id: "java.installed",
      result: {
        id: "java.installed",
        status: "warn",
        summary: "from the socket",
        checkedAt: "2026-03-01T12:00:00Z",
      },
    });
    expect(checks.result("java.installed")?.summary).toBe("from the socket");

    sockets[0]?.receive({ type: "settings", keys: ["java.keepPatches"] });
    await vi.waitFor(() => {
      expect(reload).toHaveBeenCalled();
      expect(settings.state()).toBe("ready");
    });
  });

  it("keeps session summaries current and announces starts and ends", () => {
    const store = use();
    store.connect();
    sockets[0]?.open();
    sockets[0]?.receive({ type: "session", session: { ...SESSION, state: "running" } });
    expect(store.sessions().get(SESSION.id)?.state).toBe("running");
    expect(store.transition()?.kind).toBe("started");
    expect(store.live()).toHaveLength(1);
    sockets[0]?.receive({ type: "session", session: { ...SESSION, state: "success" } });
    expect(store.transition()?.kind).toBe("ended");
    expect(store.live()).toHaveLength(0);
  });

  it("sends input as Base64 of UTF-8 only while the session runs, and resize messages", () => {
    const store = use();
    store.connect();
    sockets[0]?.open();
    store.upsert(SESSION);
    store.sendInput(SESSION.id, "pässword\r");
    expect(sockets[0]?.sent.at(-1)).toEqual({
      type: "input",
      session: SESSION.id,
      data: bytesToBase64(new TextEncoder().encode("pässword\r")),
    });
    store.sendResize(SESSION.id, 100, 40);
    expect(sockets[0]?.sent.at(-1)).toEqual({
      type: "resize",
      session: SESSION.id,
      cols: 100,
      rows: 40,
    });
    store.upsert({ ...SESSION, state: "success" });
    const before = sockets[0]?.sent.length;
    store.sendInput(SESSION.id, "x");
    expect(sockets[0]?.sent).toHaveLength(before ?? -1);
  });

  it("ignores malformed messages", () => {
    const store = use();
    store.connect();
    sockets[0]?.open();
    sockets[0]?.onmessage?.("not json");
    sockets[0]?.onmessage?.("42");
    sockets[0]?.receive({ no: "type" });
    expect(store.status()).toBe("open");
  });

  it("starts actions in the language of the UI", async () => {
    const store = use();
    const create = vi.spyOn(TestBed.inject(ApiClient), "createSession");
    await store.create({ action: "java.update", dryRun: false });
    expect(create).toHaveBeenLastCalledWith(expect.objectContaining({ lang: "en" }));
    await TestBed.inject(I18n).setLanguage("de");
    await store.create({ action: "node.update", dryRun: true });
    expect(create).toHaveBeenLastCalledWith(expect.objectContaining({ lang: "de" }));
    // An explicit language of the caller wins.
    await store.create({ action: "system.selfUpdate", lang: "fr" });
    expect(create).toHaveBeenLastCalledWith(expect.objectContaining({ lang: "fr" }));
  });
});
