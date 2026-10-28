import { inject, type Provider } from "@angular/core";
import type { WsClientMessage, WsServerMessage } from "../api/types";
import { SOCKET_FACTORY, type SocketFactory, type SocketLike } from "../ws/socket";
import { MockBackend, type MockListener } from "./mock-backend";

/**
 * A WebSocket stand-in that speaks the same protocol as the core: topics, resume with
 * fromSeq, input echo and ping. It is wired to MockBackend, which simulates the sessions.
 */
export class MockSocket implements SocketLike, MockListener {
  onopen: (() => void) | null = null;
  onmessage: ((data: string) => void) | null = null;
  onclose: (() => void) | null = null;

  private readonly topics = new Set<string>();
  private closed = false;

  constructor(private readonly backend: MockBackend) {
    backend.addListener(this);
    setTimeout(() => {
      if (!this.closed) {
        this.onopen?.();
      }
    }, 0);
  }

  send(data: string): void {
    if (this.closed) {
      return;
    }
    const message = JSON.parse(data) as WsClientMessage;
    switch (message.type) {
      case "subscribe":
        for (const topic of message.topics) {
          this.topics.add(topic);
          if (topic.startsWith("session:")) {
            const id = topic.slice("session:".length);
            for (const replayed of this.backend.replay(id, message.fromSeq ?? 0)) {
              this.push(replayed);
            }
          }
        }
        break;
      case "unsubscribe":
        for (const topic of message.topics) {
          this.topics.delete(topic);
        }
        break;
      case "input":
        this.backend.input(message.session, message.data);
        break;
      case "ping":
        this.push({ type: "pong" });
        break;
      default:
        break;
    }
  }

  deliver(message: WsServerMessage): void {
    if (this.closed || !this.wants(message)) {
      return;
    }
    this.push(message);
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.backend.removeListener(this);
    this.onclose?.();
  }

  private wants(message: WsServerMessage): boolean {
    switch (message.type) {
      case "check":
        return this.topics.has("checks");
      case "session":
        return this.topics.has("sessions") || this.topics.has(`session:${message.session.id}`);
      case "settings":
        return this.topics.has("settings");
      case "output":
      case "event":
      case "gap":
        return this.topics.has(`session:${message.session}`);
      default:
        return true;
    }
  }

  private push(message: WsServerMessage): void {
    this.onmessage?.(JSON.stringify(message));
  }
}

export const mockSocketFactoryProvider: Provider = {
  provide: SOCKET_FACTORY,
  useFactory: (): SocketFactory => {
    const backend = inject(MockBackend);
    return () => new MockSocket(backend);
  },
};
