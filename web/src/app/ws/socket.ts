import { InjectionToken } from "@angular/core";
import { environment } from "../../environments/environment";

/**
 * The part of a WebSocket the application uses. The browser socket is adapted to it, and the
 * mock socket implements it directly, so SessionsStore never knows which one it talks to.
 */
export interface SocketLike {
  onopen: (() => void) | null;
  onmessage: ((data: string) => void) | null;
  onclose: (() => void) | null;
  send(data: string): void;
  close(): void;
}

export type SocketFactory = (url: string) => SocketLike;

export const SOCKET_FACTORY = new InjectionToken<SocketFactory>("SOCKET_FACTORY", {
  providedIn: "root",
  factory: () => browserSocketFactory,
});

/** Opens the real WebSocket. */
export const browserSocketFactory: SocketFactory = (url) => {
  const ws = new WebSocket(url);
  const socket: SocketLike = {
    onopen: null,
    onmessage: null,
    onclose: null,
    send: (data) => {
      ws.send(data);
    },
    close: () => {
      ws.close();
    },
  };
  ws.onopen = () => socket.onopen?.();
  ws.onmessage = (event: MessageEvent<unknown>) => {
    if (typeof event.data === "string") {
      socket.onmessage?.(event.data);
    }
  };
  ws.onclose = () => socket.onclose?.();
  ws.onerror = () => {
    // The close event follows; reconnecting is decided there.
  };
  return socket;
};

/** ws: or wss: URL of the API socket, relative to the page address. */
export function socketUrl(location: Pick<Location, "protocol" | "host">): string {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${location.host}${environment.apiBase}/ws`;
}
