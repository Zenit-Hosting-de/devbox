import { signal } from "@angular/core";
import type { ServerMessageOf, SessionEvent, SessionEventPayload } from "../api/types";
import { base64ToBytes } from "./base64";

type StreamMessage = ServerMessageOf<"output"> | ServerMessageOf<"event">;

/**
 * The live data of one open session: terminal bytes (handed to the attached terminal), the
 * protocol events (steps, progress, result) and the sequence bookkeeping.
 *
 * Output chunks and events share one sequence. A message with a seq below the expected one
 * is a duplicate (resume, reconnect) and is dropped.
 */
export class SessionChannel {
  /** Events in order of arrival; the source of the step list. */
  readonly events = signal<readonly SessionEvent[]>([]);
  /** True while the log is being (re)loaded; stream messages wait in a queue. */
  readonly loading = signal(false);
  /**
   * Bytes of terminal output seen so far. A plugin that only speaks the protocol (steps,
   * changes, result) prints nothing to the terminal; the dock then says so.
   */
  readonly outputBytes = signal(0);
  /** The seq the next output chunk or event is expected to carry. */
  expectedSeq = 0;

  private sink: ((bytes: Uint8Array) => void) | null = null;
  private clear: (() => void) | null = null;
  private pending: Uint8Array[] = [];
  private queue: StreamMessage[] = [];
  private readonly eventSeqs = new Set<number>();

  constructor(readonly id: string) {}

  /** Connects the terminal; bytes that arrived before are written first. */
  attach(sink: (bytes: Uint8Array) => void, clear?: () => void): void {
    this.sink = sink;
    this.clear = clear ?? null;
    const pending = this.pending;
    this.pending = [];
    for (const chunk of pending) {
      sink(chunk);
    }
  }

  detach(): void {
    this.sink = null;
    this.clear = null;
  }

  write(bytes: Uint8Array): void {
    if (bytes.length === 0) {
      return;
    }
    this.outputBytes.update((n) => n + bytes.length);
    if (this.sink) {
      this.sink(bytes);
    } else {
      this.pending.push(bytes);
    }
  }

  /** Clears the terminal (and anything not yet written to it). */
  resetTerminal(): void {
    this.pending = [];
    this.outputBytes.set(0);
    this.clear?.();
  }

  beginLoading(): void {
    this.loading.set(true);
  }

  /**
   * Ends a (re)load: the log up to `nextSeq` is in the terminal and `events` are known.
   * Queued stream messages are then applied (those below nextSeq are dropped).
   */
  finishLoading(nextSeq: number, events: readonly SessionEvent[]): void {
    this.eventSeqs.clear();
    for (const entry of events) {
      this.eventSeqs.add(entry.seq);
    }
    this.events.set([...events].sort((a, b) => a.seq - b.seq));
    this.expectedSeq = nextSeq;
    this.loading.set(false);
    const queue = this.queue;
    this.queue = [];
    for (const message of queue) {
      this.handle(message);
    }
  }

  /** Applies one stream message. */
  handle(message: StreamMessage): void {
    if (this.loading()) {
      this.queue.push(message);
      return;
    }
    if (message.seq < this.expectedSeq) {
      return;
    }
    this.expectedSeq = message.seq + 1;
    if (message.type === "output") {
      this.write(base64ToBytes(message.data));
      return;
    }
    if (this.eventSeqs.has(message.seq)) {
      return;
    }
    this.eventSeqs.add(message.seq);
    const entry: SessionEvent = {
      seq: message.seq,
      time: message.time,
      event: message.event as unknown as SessionEventPayload,
    };
    this.events.update((list) => [...list, entry]);
  }
}
