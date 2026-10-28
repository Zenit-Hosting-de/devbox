import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import type { CheckSummary } from "../api/types";
import { MockBackend } from "../mock/mock-backend";
import { byText, queryAll, setUpApp, settle } from "../testing/test-app";
import { ChecksStore } from "../stores/checks.store";
import { DockStore } from "../stores/dock.store";
import { SessionsStore } from "../stores/sessions.store";
import { OverviewPage } from "./overview";

describe("OverviewPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  it("shows skeletons while loading and then a tile per plugin, grouped by category", async () => {
    backend.latency = 20;
    const fixture = TestBed.createComponent(OverviewPage);
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector("z-skeleton")).not.toBeNull();
    expect(fixture.nativeElement.querySelector("[aria-busy=true]")).not.toBeNull();

    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    expect(fixture.nativeElement.querySelector("z-skeleton")).toBeNull();

    const headings = queryAll(fixture, "section h2").map((h) => h.textContent?.trim());
    expect(headings).toEqual(["Core", "Runtimes", "Tools"]);
  });

  it("shows the check badge, the summary, hints with their action and the primary action", async () => {
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });

    const java = byText(fixture, "z-panel", "Java");
    expect(java).toBeDefined();
    expect(java?.textContent).toContain("OK");
    expect(java?.textContent).toContain("2 JDKs installed, default 21");
    expect(java?.querySelector("z-badge")?.classList.contains("z-badge--success")).toBe(true);
    // Primary action of the manifest (contributes.overview.primaryAction).
    expect(byText(fixture, "z-panel button", "Update and clean up")).toBeDefined();
    // Link to the detail page.
    expect(java?.querySelector("a[href='/plugins/java']")).not.toBeNull();
  });

  it("shows state and translated reasons of a plugin that is not ready, without actions", async () => {
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    const docker = byText(fixture, "z-panel", "Docker");
    expect(docker?.textContent).toContain("Blocked");
    expect(docker?.textContent).toContain("Command missing");
    expect(docker?.textContent).toContain("systemctl");
    expect(docker?.textContent).toContain("Requires plugin");
    expect(docker?.querySelector(".z-btn--primary")).toBeNull();
  });

  it("shows hints with a direct action; the action starts a session and opens a dock tab", async () => {
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    TestBed.inject(ChecksStore).results.update((all) => ({
      ...all,
      "java.installed": {
        ...(all["java.installed"] as CheckSummary),
        hints: [{ text: "An update is available.", action: "java.update" }],
      },
    }));
    await settle(fixture);
    const java = byText(fixture, "z-panel", "Java");
    expect(java?.textContent).toContain("An update is available.");
    const hintButton = [...(java?.querySelectorAll("button") ?? [])].find(
      (b) => b.closest(".hint") !== null,
    );
    expect(hintButton?.textContent).toContain("Update and clean up");
    hintButton?.click();
    await vi.waitFor(() => {
      expect(TestBed.inject(DockStore).tabs()).toHaveLength(1);
    });
    expect(TestBed.inject(SessionsStore).sessions().size).toBe(1);
  });

  it("refreshes the checks with one POST and shows progress until the check messages arrive", async () => {
    const api = TestBed.inject(ApiClient);
    const refresh = vi.spyOn(api, "refreshChecks");
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    byText(fixture, "button", "Refresh checks")?.click();
    await settle(fixture);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledWith(undefined);
  });

  it("shows an alert with a retry when the core cannot be reached", async () => {
    const api = TestBed.inject(ApiClient);
    vi.spyOn(api, "plugins").mockRejectedValueOnce(
      Object.assign(new Error("down"), { code: "network", status: 0, errors: [] }),
    );
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(fixture.nativeElement.querySelector("z-alert")).not.toBeNull();
    });
    expect(fixture.nativeElement.textContent).toContain("Data could not be loaded");
    byText(fixture, "z-alert button", "Try again")?.click();
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
  });
});
