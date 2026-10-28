import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import type { Session } from "../api/types";
import { MockBackend } from "../mock/mock-backend";
import { DockStore } from "../stores/dock.store";
import { byText, fakeTerminals, queryAll, setUpApp, settle } from "../testing/test-app";
import { HistoryDetailPage } from "./history-detail";
import { HistoryPage } from "./history";

const SESSION_ID = "s_0000000SZ0";

function rows(fixture: Parameters<typeof queryAll>[0]): HTMLElement[] {
  return queryAll(fixture, "[zrow], .z-row");
}

describe("HistoryPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  it("lists the sessions with action, state, dry run, times, changes and summary", async () => {
    const fixture = TestBed.createComponent(HistoryPage);
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(12);
    });
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain("Update and clean up");
    expect(text).toContain("Succeeded");
    expect(text).toContain("Failed");
    expect(text).toContain("Stopped");
    expect(text).toContain("Dry run");
    expect(text).toContain("Download failed: connection reset");
    // Dates use the Intl format of the current language.
    expect(text).toMatch(/\d{1,2}\/\d{1,2}\/\d{2}, \d{1,2}:\d{2}/);
  });

  it("filters by state and by plugin", async () => {
    const fixture = TestBed.createComponent(HistoryPage);
    const api = TestBed.inject(ApiClient);
    const list = vi.spyOn(api, "sessions");
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(12);
    });

    const state = fixture.nativeElement.querySelector("#hist-state") as HTMLSelectElement;
    state.value = "failed";
    state.dispatchEvent(new Event("change", { bubbles: true }));
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(2);
    });
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ state: "failed" }));

    state.value = "";
    state.dispatchEvent(new Event("change", { bubbles: true }));
    const plugin = fixture.nativeElement.querySelector("#hist-plugin") as HTMLSelectElement;
    plugin.value = "node";
    plugin.dispatchEvent(new Event("change", { bubbles: true }));
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(4);
    });
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ plugin: "node" }));
    expect(list.mock.calls.at(-1)?.[0]).not.toHaveProperty("state");
  });

  it("loads more with the cursor and hides the button at the end", async () => {
    const api = TestBed.inject(ApiClient);
    const page = (id: string, next?: string) => ({
      sessions: [{ id, action: "java.update", state: "success", dryRun: false } as Session],
      ...(next ? { next } : {}),
    });
    const list = vi
      .spyOn(api, "sessions")
      .mockResolvedValueOnce(page("s_AAAAAAAAAA", "cursor-1"))
      .mockResolvedValueOnce(page("s_BBBBBBBBBB"));
    const fixture = TestBed.createComponent(HistoryPage);
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(1);
    });
    byText(fixture, "button", "Load more")?.click();
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(2);
    });
    expect(list.mock.calls[1]?.[0]).toMatchObject({ cursor: "cursor-1" });
    expect(byText(fixture, "button", "Load more")).toBeUndefined();
  });

  it("offers to open a running session in the dock", async () => {
    backend.stepMs = 500;
    const running = backend.createSession({ action: "java.update" });
    const fixture = TestBed.createComponent(HistoryPage);
    await settle(fixture, () => {
      expect(rows(fixture).length).toBe(13);
    });
    byText(fixture, "button", "Open in terminal")?.click();
    await vi.waitFor(() => {
      expect(TestBed.inject(DockStore).active()).toBe(running.id);
    });
    backend.cancelSession(running.id);
  });
});

describe("HistoryDetailPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  it("shows the details, the steps and the log of a finished session in a read-only terminal", async () => {
    const finished = backend.listSessions({ state: "success" }).sessions[0] as Session;
    const fixture = TestBed.createComponent(HistoryDetailPage);
    fixture.componentRef.setInput("id", finished.id);
    await settle(fixture, () => {
      expect(fakeTerminals).toHaveLength(1);
      expect(fakeTerminals[0]?.text).toContain("==>");
    });
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain(finished.action);
    expect(text).toContain("Succeeded");
    expect(text).toContain("Exit code");
    expect(text).toContain("Command line");
    // History sessions carry no events.
    expect(text).toContain("No steps recorded.");
    expect(TestBed.inject(DockStore).tabs()).toHaveLength(0);
  });

  it("opens a running session in the dock instead", async () => {
    backend.stepMs = 500;
    const running = backend.createSession({ action: "node.update" });
    const fixture = TestBed.createComponent(HistoryDetailPage);
    fixture.componentRef.setInput("id", running.id);
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("The session is still running");
    });
    await vi.waitFor(() => {
      expect(
        TestBed.inject(DockStore)
          .tabs()
          .map((t) => t.id),
      ).toEqual([running.id]);
    });
    backend.cancelSession(running.id);
  });

  it("shows an error for an unknown session", async () => {
    const fixture = TestBed.createComponent(HistoryDetailPage);
    fixture.componentRef.setInput("id", SESSION_ID);
    await settle(fixture, () => {
      expect(fixture.nativeElement.querySelector("z-alert")).not.toBeNull();
    });
    expect(fixture.nativeElement.textContent).toContain("Not found.");
  });
});
