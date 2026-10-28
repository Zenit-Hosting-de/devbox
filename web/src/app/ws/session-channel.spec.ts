import { describe, expect, it } from "vitest";
import type { ServerMessageOf, SessionEvent } from "../api/types";
import { bytesToBase64 } from "./base64";
import { SessionChannel } from "./session-channel";
import { summarizeEvents } from "./events";

const ID = "s_0123456789";

function output(seq: number, text: string): ServerMessageOf<"output"> {
  return {
    type: "output",
    session: ID,
    seq,
    data: bytesToBase64(new TextEncoder().encode(text)),
  };
}

function event(seq: number, payload: Record<string, unknown>): ServerMessageOf<"event"> {
  return {
    type: "event",
    session: ID,
    seq,
    time: "2026-03-01T12:00:00Z",
    event: payload,
  } as unknown as ServerMessageOf<"event">;
}

describe("SessionChannel", () => {
  it("hands output chunks to the terminal in order and counts the bytes", () => {
    const channel = new SessionChannel(ID);
    const seen: string[] = [];
    channel.attach((bytes) => seen.push(new TextDecoder().decode(bytes)));
    channel.handle(output(0, "line 1\r\n"));
    channel.handle(output(1, "line 2\r\n"));
    expect(seen).toEqual(["line 1\r\n", "line 2\r\n"]);
    expect(channel.outputBytes()).toBe(16);
  });

  it("keeps output that arrives before the terminal exists and writes it on attach", () => {
    const channel = new SessionChannel(ID);
    channel.handle(output(0, "early\r\n"));
    const seen: string[] = [];
    channel.attach((bytes) => seen.push(new TextDecoder().decode(bytes)));
    expect(seen).toEqual(["early\r\n"]);
  });

  it("reports no output for a plugin that only sends protocol events", () => {
    const channel = new SessionChannel(ID);
    channel.handle(event(0, { type: "step", id: "a", label: "A", state: "running" }));
    channel.handle(
      event(1, { type: "change", kind: "file", op: "create", target: "t", summary: "s" }),
    );
    expect(channel.outputBytes()).toBe(0);
    expect(channel.events()).toHaveLength(2);
  });

  it("starts counting again after the terminal was reset", () => {
    const channel = new SessionChannel(ID);
    channel.write(new TextEncoder().encode("abc"));
    expect(channel.outputBytes()).toBe(3);
    channel.resetTerminal();
    expect(channel.outputBytes()).toBe(0);
  });
});

describe("summarizeEvents", () => {
  it("collects changes with target and summary, and the log messages", () => {
    const events: SessionEvent[] = [
      event(0, { type: "change", kind: "file", op: "update", target: "~/.npmrc", summary: "set" }),
      event(1, { type: "log", level: "warn", text: "careful" }),
      event(2, { type: "result", summary: "done" }),
    ].map((m) => ({
      seq: m.seq,
      time: m.time,
      event: m.event as unknown as SessionEvent["event"],
    }));
    const summary = summarizeEvents(events);
    expect(summary.changes).toEqual([
      { kind: "file", op: "update", target: "~/.npmrc", summary: "set" },
    ]);
    expect(summary.logs).toEqual([{ level: "warn", text: "careful" }]);
    expect(summary.result).toEqual({ summary: "done" });
  });
});
