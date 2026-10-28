import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ServerMessageOf, Session } from "../api/types";
import { MockBackend } from "../mock/mock-backend";
import { DockStore } from "../stores/dock.store";
import { InfoStore } from "../stores/info.store";
import { SessionsStore } from "../stores/sessions.store";
import { byText, fakeTerminals, query, queryAll, setUpApp, settle } from "../testing/test-app";
import type { SessionChannel } from "../ws/session-channel";
import { Dock } from "./dock";

describe("Dock (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  async function start(action: string, params: Record<string, unknown> = {}) {
    const sessions = TestBed.inject(SessionsStore);
    sessions.connect();
    const session = await sessions.create({ action, params, dryRun: false });
    TestBed.inject(DockStore).openNew(session);
    return session;
  }

  it("opens a tab per session and streams coloured output into the terminal", async () => {
    const fixture = TestBed.createComponent(Dock);
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("No session open");

    const session = await start("java.update");
    await settle(fixture, () => {
      expect(queryAll(fixture, "[role=tab]")).toHaveLength(1);
      expect(fakeTerminals).toHaveLength(1);
      expect(fakeTerminals[0]?.text).toContain("Finished, 1 change applied");
    });
    expect(fakeTerminals[0]?.text).toContain("\u001b[1;34m==> Update and clean up");
    // Progress and steps come from the protocol events, the result from the session summary.
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("Succeeded");
    });
    expect(fixture.nativeElement.textContent).toContain("3 steps");
    expect(fixture.nativeElement.textContent).toContain("Finished, 1 change applied");
    expect(
      TestBed.inject(DockStore)
        .tabs()
        .map((t) => t.id),
    ).toEqual([session.id]);
  });

  it("sends keyboard input only while the session runs and echoes it (sudo prompt)", async () => {
    const fixture = TestBed.createComponent(Dock);
    await settle(fixture);
    backend.stepMs = 20;
    await start("system.selfUpdate");
    await settle(fixture, () => {
      expect(fakeTerminals[0]?.text).toContain("[sudo] password for dev:");
    });
    // The session waits for the password; typing is sent and the mock echoes it masked.
    fakeTerminals[0]?.type("s3cret\r");
    await settle(fixture, () => {
      expect(fakeTerminals[0]?.text).toContain("******");
      expect(fakeTerminals[0]?.text).toContain("Finished");
    });
    expect(fakeTerminals[0]?.text).not.toContain("s3cret");
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("Succeeded");
    });
    // After the end typing is dropped (no running session).
    const sent = vi.spyOn(TestBed.inject(SessionsStore), "sendInput");
    fakeTerminals[0]?.type("late");
    expect(sent).not.toHaveBeenCalled();
  });

  it("cancels a running session", async () => {
    const fixture = TestBed.createComponent(Dock);
    await settle(fixture);
    backend.stepMs = 200;
    await start("java.update");
    await settle(fixture, () => {
      expect(byText(fixture, "button", "Cancel")).toBeDefined();
    });
    byText(fixture, "button", "Cancel")?.click();
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("Stopped");
    });
    expect(fakeTerminals[0]?.text).toContain("Cancelled");
    expect(byText(fixture, "button", "Cancel")).toBeUndefined();
  });

  it("closes tabs, selects the next one and collapses", async () => {
    const fixture = TestBed.createComponent(Dock);
    await settle(fixture);
    const first = await start("java.update");
    const second = await start("node.update");
    await settle(fixture, () => {
      expect(queryAll(fixture, "[role=tab]")).toHaveLength(2);
    });
    const dock = TestBed.inject(DockStore);
    expect(dock.active()).toBe(second.id);
    dock.select(first.id);
    expect(dock.active()).toBe(first.id);
    dock.close(first.id);
    expect(dock.active()).toBe(second.id);
    await settle(fixture);
    expect(queryAll(fixture, "[role=tab]")).toHaveLength(1);

    dock.toggle();
    await settle(fixture);
    expect(query(fixture, "#dock-panels")?.hasAttribute("hidden")).toBe(true);
    expect(localStorage.getItem("devbox.dock.collapsed")).toBe("1");
  });

  it("keeps only tabs inside the tablist and moves between them with the keyboard", async () => {
    const fixture = TestBed.createComponent(Dock);
    await settle(fixture);
    expect(query(fixture, "[role=tablist]")).toBeNull();
    expect(fixture.nativeElement.textContent).toContain("No session open");

    const first = await start("java.update");
    const second = await start("node.update");
    await settle(fixture, () => {
      expect(queryAll(fixture, "[role=tab]")).toHaveLength(2);
    });
    const list = query(fixture, "[role=tablist]") as HTMLElement;
    expect([...list.children].every((c) => c.getAttribute("role") === "tab")).toBe(true);
    expect(query(fixture, "[role=tablist] button:not([role=tab])")).toBeNull();

    const tabs = queryAll<HTMLButtonElement>(fixture, "[role=tab]");
    expect(tabs.map((t) => t.tabIndex)).toEqual([-1, 0]);
    const press = async (target: HTMLElement, key: string) => {
      target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
      await settle(fixture);
    };
    const dock = TestBed.inject(DockStore);
    await press(tabs[1] as HTMLElement, "ArrowRight");
    expect(dock.active()).toBe(first.id);
    expect(document.activeElement?.id).toBe(`dock-tab-${first.id}`);
    await press(tabs[0] as HTMLElement, "End");
    expect(dock.active()).toBe(second.id);
    await press(tabs[1] as HTMLElement, "Home");
    expect(dock.active()).toBe(first.id);
    await press(tabs[0] as HTMLElement, "ArrowLeft");
    expect(dock.active()).toBe(second.id);
    expect(queryAll<HTMLButtonElement>(fixture, "[role=tab]").map((t) => t.tabIndex)).toEqual([
      -1, 0,
    ]);

    // The close button of the active tab sits next to the tablist, not inside it.
    const closeButton = query<HTMLButtonElement>(fixture, ".tabs > button");
    expect(closeButton?.getAttribute("aria-label")).toContain("Close");
    closeButton?.click();
    await settle(fixture);
    expect(dock.tabs().map((t) => t.id)).toEqual([first.id]);
    await press(query(fixture, "[role=tab]") as HTMLElement, "Delete");
    expect(dock.tabs()).toEqual([]);
  });

  it("reports the terminal size and sends resize messages for a running session", async () => {
    const fixture = TestBed.createComponent(Dock);
    await settle(fixture);
    backend.stepMs = 200;
    await start("java.update");
    await settle(fixture, () => {
      expect(fakeTerminals).toHaveLength(1);
    });
    fakeTerminals[0]?.resize({ cols: 90, rows: 20 });
    await vi.waitFor(() => {
      expect(TestBed.inject(DockStore).size()).toEqual({ cols: 90, rows: 20 });
    });
  });

  describe("a plugin that only speaks the protocol", () => {
    const SESSION = {
      id: "s_0123456789",
      action: "java.update",
      title: "Update",
      state: "success",
      dryRun: false,
    } as const;

    function openQuiet(session: Session = SESSION): SessionChannel {
      const sessions = TestBed.inject(SessionsStore);
      sessions.upsert(session);
      const channel = sessions.openNew(session);
      TestBed.inject(DockStore).openNew(session);
      return channel;
    }

    function protocol(seq: number, event: Record<string, unknown>): void {
      TestBed.inject(SessionsStore)
        .channel(SESSION.id)
        ?.handle({
          type: "event",
          session: SESSION.id,
          seq,
          time: "2026-03-01T12:00:00Z",
          event,
        } as unknown as ServerMessageOf<"event">);
    }

    it("lists the changes with target and summary, and says that the terminal is empty", async () => {
      const fixture = TestBed.createComponent(Dock);
      await TestBed.inject(InfoStore).load(); // the home directory
      openQuiet();
      protocol(0, { type: "step", id: "install", label: "Node.js 24", state: "done" });
      protocol(1, {
        type: "change",
        kind: "file",
        op: "create",
        target: "/home/dev/.local/share/node/24.1.0",
        summary: "Node.js 24.1.0",
      });
      protocol(2, {
        type: "change",
        kind: "link",
        op: "update",
        target: "node",
        summary: "points to 24",
      });
      await settle(fixture, () => {
        expect(queryAll(fixture, ".changes li")).toHaveLength(2);
      });
      const text = fixture.nativeElement.textContent as string;
      expect(text).toContain("2 changes");
      // The home directory is written as ~, the full path is the tooltip.
      expect(text).toContain("~/.local/share/node/24.1.0");
      expect(text).toContain("Node.js 24.1.0");
      expect(text).toContain("points to 24");
      expect(text).toContain("Created");
      expect(text).toContain("Changed");
      expect(text).toContain("No terminal output");
      expect(fakeTerminals[0]?.text).toBe("");
    });

    it("shows planned changes in a dry run and the messages of the plugin", async () => {
      const fixture = TestBed.createComponent(Dock);
      openQuiet({ ...SESSION, dryRun: true });
      protocol(0, {
        type: "change",
        kind: "file",
        op: "delete",
        target: "x",
        summary: "would remove",
      });
      protocol(1, { type: "log", level: "error", text: "distribution server not reachable" });
      await settle(fixture, () => {
        expect(queryAll(fixture, ".changes li")).toHaveLength(2);
      });
      const text = fixture.nativeElement.textContent as string;
      expect(text).toContain("1 planned change");
      expect(text).toContain("Removed");
      expect(text).toContain("Error");
      expect(text).toContain("distribution server not reachable");
    });

    it("does not claim missing output once the terminal got some", async () => {
      const fixture = TestBed.createComponent(Dock);
      const channel = openQuiet();
      channel.write(new TextEncoder().encode("hello\r\n"));
      protocol(0, { type: "change", kind: "file", op: "create", target: "x", summary: "y" });
      await settle(fixture, () => {
        expect(queryAll(fixture, ".changes li")).toHaveLength(1);
      });
      expect(fixture.nativeElement.textContent).not.toContain("No terminal output");
      expect(fakeTerminals[0]?.text).toContain("hello");
    });
  });
});
