import { TestBed } from "@angular/core/testing";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import type { Info } from "../api/types";
import { SOCKET_FACTORY, type SocketLike } from "../ws/socket";
import { setUpApp } from "../testing/test-app";
import { InfoStore } from "./info.store";
import { PAGE_RELOAD, RestartStore } from "./restart.store";
import { BACKOFF_START_MS, SessionsStore } from "./sessions.store";

class FakeSocket implements SocketLike {
  onopen: (() => void) | null = null;
  onmessage: ((data: string) => void) | null = null;
  onclose: (() => void) | null = null;
  readonly sent: unknown[] = [];

  send(data: string): void {
    this.sent.push(JSON.parse(data));
  }
  close(): void {
    this.onclose?.();
  }
  receive(message: unknown): void {
    this.onmessage?.(JSON.stringify(message));
  }
  open(): void {
    this.onopen?.();
  }
  drop(): void {
    this.onclose?.();
  }
}

describe("RestartStore", () => {
  let sockets: FakeSocket[];
  let reloads: number;

  /** An application whose core reports the given versions, one per call of /info. */
  function use(...versions: string[]): { sessions: SessionsStore; restart: RestartStore } {
    sockets = [];
    reloads = 0;
    setUpApp([
      {
        provide: SOCKET_FACTORY,
        useValue: (): SocketLike => {
          const socket = new FakeSocket();
          sockets.push(socket);
          return socket;
        },
      },
      { provide: PAGE_RELOAD, useValue: () => reloads++ },
    ]);
    const api = TestBed.inject(ApiClient);
    const infos = vi.spyOn(api, "info");
    const base = {
      vendor: "zenit-hosting",
      protocol: 1,
      auth: "local",
      host: {},
    } as unknown as Info;
    for (const version of versions) {
      infos.mockResolvedValueOnce({ ...base, version });
    }
    return { sessions: TestBed.inject(SessionsStore), restart: TestBed.inject(RestartStore) };
  }

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the banner on the restart message and remembers the version", async () => {
    const { sessions, restart } = use("1.0.0");
    await TestBed.inject(InfoStore).load();
    sessions.connect();
    sockets[0]?.open();
    expect(restart.restarting()).toBe(false);

    sockets[0]?.receive({ type: "restart" });
    expect(restart.restarting()).toBe(true);
    expect(reloads).toBe(0);
  });

  it("reconnects with backoff, reloads /info and reloads the page when the version changed", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const { sessions, restart } = use("1.0.0", "2.0.0");
    await TestBed.inject(InfoStore).load();
    sessions.connect();
    sockets[0]?.open();
    sockets[0]?.receive({ type: "restart" });

    sockets[0]?.drop(); // the core closes with 1012 and exits
    expect(sessions.status()).toBe("offline");
    expect(restart.restarting()).toBe(true);
    vi.advanceTimersByTime(BACKOFF_START_MS);
    expect(sockets).toHaveLength(2);
    sockets[1]?.drop(); // the new process is not listening yet: the next try follows
    vi.advanceTimersByTime(BACKOFF_START_MS * 2);
    expect(sockets).toHaveLength(3);
    sockets[2]?.open();
    await vi.waitFor(() => {
      expect(reloads).toBe(1);
    });
    expect(TestBed.inject(InfoStore).info()?.version).toBe("2.0.0");
    expect(restart.restarting()).toBe(true); // the banner stays until the page is gone
  });

  it("hides the banner without reloading when the version is the same (the restart did not happen)", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const { sessions, restart } = use("1.0.0", "1.0.0");
    await TestBed.inject(InfoStore).load();
    sessions.connect();
    sockets[0]?.open();
    sockets[0]?.receive({ type: "restart" });
    sockets[0]?.drop();
    vi.advanceTimersByTime(BACKOFF_START_MS);
    sockets[1]?.open();
    await vi.waitFor(() => {
      expect(restart.restarting()).toBe(false);
    });
    expect(reloads).toBe(0);
  });

  it("does nothing on a plain reconnect", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const { sessions, restart } = use("1.0.0", "2.0.0");
    await TestBed.inject(InfoStore).load();
    sessions.connect();
    sockets[0]?.open();
    sockets[0]?.drop();
    vi.advanceTimersByTime(BACKOFF_START_MS);
    sockets[1]?.open();
    await vi.advanceTimersByTimeAsync(0);
    expect(restart.restarting()).toBe(false);
    expect(reloads).toBe(0);
  });
});
