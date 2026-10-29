import { DOCUMENT } from "@angular/common";
import { Injectable, computed, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import type {
  ServerMessageOf,
  Session,
  SessionEvent,
  SessionId,
  WsClientMessage,
  WsServerMessage,
} from "../api/types";
import { isLive } from "../api/types";
import { I18n } from "../i18n/i18n";
import { SOCKET_FACTORY, socketUrl, type SocketLike } from "../ws/socket";
import { SessionChannel } from "../ws/session-channel";
import { utf8ToBase64 } from "../ws/base64";

export type ConnectionStatus = "idle" | "connecting" | "open" | "offline";

/** Reconnect delays: 1 s, doubling up to 30 s. */
export const BACKOFF_START_MS = 1000;
export const BACKOFF_MAX_MS = 30_000;

const TOPICS = ["checks", "sessions", "settings", "plugins"] as const;

type MessageType = WsServerMessage["type"];
type Handler<T extends MessageType> = (message: ServerMessageOf<T>) => void;
type AnyHandler = (message: never) => void;

export interface SessionTransition {
  kind: "started" | "ended";
  session: Session;
}

/**
 * The only owner of the WebSocket. It reconnects with backoff, resubscribes after a
 * reconnect, keeps the live session summaries and the channels of opened sessions, and
 * hands every other message to the stores that registered for its type.
 */
@Injectable({ providedIn: "root" })
export class SessionsStore {
  private readonly api = inject(ApiClient);
  private readonly factory = inject(SOCKET_FACTORY);
  private readonly doc = inject(DOCUMENT);
  private readonly i18n = inject(I18n);

  readonly status = signal<ConnectionStatus>("idle");
  /** Summaries of sessions seen so far (REST and WebSocket), keyed by ID. */
  readonly sessions = signal<ReadonlyMap<string, Session>>(new Map());
  /** Sessions that are queued or running, newest first. */
  readonly live = computed(() => this.sortedSessions().filter((s) => isLive(s.state)));
  /** The latest start or end of a session, for the announcement in the live region. */
  readonly transition = signal<SessionTransition | null>(null);

  private readonly sortedSessions = computed(() =>
    [...this.sessions().values()].sort((a, b) =>
      (b.startedAt ?? "").localeCompare(a.startedAt ?? ""),
    ),
  );

  private socket: SocketLike | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private attempt = 0;
  private wasOpen = false;
  private wanted = false;
  private readonly channels = new Map<string, SessionChannel>();
  private readonly handlers = new Map<MessageType, AnyHandler[]>();
  private readonly reconnectHooks: (() => void | Promise<void>)[] = [];

  // -------------------------------------------------------------------------
  // Connection
  // -------------------------------------------------------------------------

  /** Opens the connection (once) and keeps it open. */
  connect(): void {
    this.wanted = true;
    if (this.socket || this.timer !== undefined) {
      return;
    }
    this.open();
  }

  /** Closes the connection for good (tests, teardown). */
  disconnect(): void {
    this.wanted = false;
    clearTimeout(this.timer);
    this.timer = undefined;
    const socket = this.socket;
    this.socket = null;
    socket?.close();
    this.status.set("idle");
  }

  /** Registers a handler for one message type; other stores receive WebSocket data this way. */
  on<T extends MessageType>(type: T, handler: Handler<T>): void {
    const list = this.handlers.get(type) ?? [];
    list.push(handler as unknown as AnyHandler);
    this.handlers.set(type, list);
  }

  /** Registers a function that reloads a store over REST after the connection came back. */
  onReconnect(hook: () => void | Promise<void>): void {
    this.reconnectHooks.push(hook);
  }

  private open(): void {
    this.timer = undefined;
    this.status.set(this.wasOpen ? "offline" : "connecting");
    const socket = this.factory(socketUrl(this.doc.location));
    this.socket = socket;
    socket.onopen = () => {
      if (this.socket !== socket) {
        return;
      }
      this.attempt = 0;
      this.status.set("open");
      this.subscribeAll();
      if (this.wasOpen) {
        void this.resync();
      }
      this.wasOpen = true;
    };
    socket.onmessage = (data) => {
      if (this.socket === socket) {
        this.receive(data);
      }
    };
    socket.onclose = () => {
      if (this.socket !== socket) {
        return;
      }
      this.socket = null;
      this.status.set("offline");
      if (this.wanted) {
        this.scheduleReconnect();
      }
    };
  }

  private scheduleReconnect(): void {
    const delay = Math.min(BACKOFF_START_MS * 2 ** this.attempt, BACKOFF_MAX_MS);
    this.attempt++;
    this.timer = setTimeout(() => {
      this.open();
    }, delay);
  }

  private send(message: WsClientMessage): void {
    if (this.socket && this.status() === "open") {
      this.socket.send(JSON.stringify(message));
    }
  }

  private subscribeAll(): void {
    this.send({ type: "subscribe", topics: [...TOPICS] });
    for (const channel of this.channels.values()) {
      this.subscribeChannel(channel);
    }
  }

  private subscribeChannel(channel: SessionChannel): void {
    if (channel.loading()) {
      return;
    }
    this.send({
      type: "subscribe",
      topics: [`session:${channel.id}`],
      fromSeq: channel.expectedSeq,
    });
  }

  /** After a reconnect: full state over REST first, the subscriptions already resumed. */
  private async resync(): Promise<void> {
    const tasks: Promise<unknown>[] = [this.reloadSessions()];
    for (const hook of this.reconnectHooks) {
      tasks.push(Promise.resolve().then(hook));
    }
    await Promise.allSettled(tasks);
  }

  private async reloadSessions(): Promise<void> {
    const list = await this.api.sessions({ limit: 100 });
    for (const session of list.sessions) {
      this.upsert(session);
    }
  }

  // -------------------------------------------------------------------------
  // Incoming messages
  // -------------------------------------------------------------------------

  private receive(raw: string): void {
    let message: WsServerMessage;
    try {
      message = JSON.parse(raw) as WsServerMessage;
    } catch {
      return;
    }
    if (typeof message !== "object" || message === null || typeof message.type !== "string") {
      return;
    }
    switch (message.type) {
      case "ping":
        this.send({ type: "pong" });
        return;
      case "pong":
        return;
      case "session":
        this.upsert(message.session, true);
        break;
      case "output":
      case "event":
        this.channels.get(message.session)?.handle(message);
        return;
      case "gap":
        void this.reload(message.session);
        return;
      default:
        break;
    }
    for (const handler of this.handlers.get(message.type) ?? []) {
      (handler as (m: WsServerMessage) => void)(message);
    }
  }

  // -------------------------------------------------------------------------
  // Session summaries
  // -------------------------------------------------------------------------

  /** Stores a summary; `announce` records starts and ends for the live region. */
  upsert(session: Session, announce = false): void {
    const previous = this.sessions().get(session.id);
    const next = new Map(this.sessions());
    next.set(session.id, session);
    this.sessions.set(next);
    if (!announce) {
      return;
    }
    if (session.state === "running" && previous?.state !== "running") {
      this.transition.set({ kind: "started", session });
    } else if (!isLive(session.state) && previous?.state !== session.state) {
      this.transition.set({ kind: "ended", session });
    }
  }

  /** The summary of a session, loaded when unknown. */
  async session(id: string): Promise<Session> {
    const known = this.sessions().get(id);
    if (known) {
      return known;
    }
    const loaded = await this.api.session(id);
    this.upsert(loaded);
    return loaded;
  }

  async create(body: Parameters<ApiClient["createSession"]>[0]): Promise<Session> {
    // The action runs in the language of the UI (DEVBOX_LANG), unless the caller says otherwise.
    const session = await this.api.createSession({ lang: this.i18n.language(), ...body });
    this.upsert(session);
    this.transition.set({ kind: "started", session });
    return session;
  }

  async cancel(id: string): Promise<Session> {
    const session = await this.api.cancelSession(id);
    this.upsert(session);
    return session;
  }

  // -------------------------------------------------------------------------
  // Channels of opened sessions
  // -------------------------------------------------------------------------

  channel(id: string): SessionChannel | undefined {
    return this.channels.get(id);
  }

  /** A session that was just created: its output is streamed from the first chunk. */
  openNew(session: Session): SessionChannel {
    const existing = this.channels.get(session.id);
    if (existing) {
      return existing;
    }
    const channel = new SessionChannel(session.id);
    this.channels.set(session.id, channel);
    this.subscribeChannel(channel);
    return channel;
  }

  /**
   * A session that already runs: the log so far is written into the terminal, then the
   * stream is subscribed from the sequence number the log ends at.
   */
  async openExisting(session: Session): Promise<SessionChannel> {
    this.upsert(session);
    const existing = this.channels.get(session.id);
    if (existing) {
      return existing;
    }
    const channel = new SessionChannel(session.id);
    this.channels.set(session.id, channel);
    channel.beginLoading();
    await this.load(channel);
    this.subscribeChannel(channel);
    return channel;
  }

  closeChannel(id: string): void {
    const channel = this.channels.get(id);
    if (!channel) {
      return;
    }
    this.channels.delete(id);
    channel.detach();
    this.send({ type: "unsubscribe", topics: [`session:${id}`] });
  }

  /** Log first, then events: the stream resumes at the log's end and events are deduplicated. */
  private async load(channel: SessionChannel): Promise<void> {
    let nextSeq = 0;
    let events: SessionEvent[] = [];
    try {
      const log = await this.api.sessionLog(channel.id);
      nextSeq = log.nextSeq;
      channel.write(log.data);
      const list = await this.api.sessionEvents(channel.id);
      events = list.events;
    } catch {
      // The stream still continues from what is known; the terminal shows what arrived.
    } finally {
      channel.finishLoading(nextSeq, events);
    }
  }

  /** The requested sequence is older than the buffer of the core: reset and reload the log. */
  private async reload(id: SessionId): Promise<void> {
    const channel = this.channels.get(id);
    if (!channel) {
      return;
    }
    channel.beginLoading();
    channel.resetTerminal();
    await this.load(channel);
  }

  // -------------------------------------------------------------------------
  // Terminal input
  // -------------------------------------------------------------------------

  sendInput(id: string, text: string): void {
    const session = this.sessions().get(id);
    if (session && !isLive(session.state)) {
      return;
    }
    this.send({ type: "input", session: id, data: utf8ToBase64(text) });
  }

  sendResize(id: string, cols: number, rows: number): void {
    this.send({ type: "resize", session: id, cols, rows });
  }
}
