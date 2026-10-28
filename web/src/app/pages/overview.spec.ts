import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import type { CheckSummary } from "../api/types";
import { I18n } from "../i18n/i18n";
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
    // The button carries the label of the action; the English hint text stays out of the
    // way, so a translated UI does not show it.
    expect(java?.textContent).not.toContain("An update is available.");
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
    expect(refresh).toHaveBeenCalledWith(undefined, "en");
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

  it("puts the groups into a responsive column layout and keeps the group headings", async () => {
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    const container = fixture.nativeElement.querySelector(".groups") as HTMLElement;
    expect(container).not.toBeNull();
    const sections = queryAll(container, ":scope > section.group");
    expect(sections).toHaveLength(3);
    // Every group is a labelled region with its heading and its tiles.
    for (const section of sections) {
      const heading = section.querySelector("h2");
      expect(heading?.id).toBeTruthy();
      expect(section.getAttribute("aria-labelledby")).toBe(heading?.id);
      expect(section.querySelectorAll(".tiles > z-panel").length).toBeGreaterThan(0);
    }
  });

  it("shows the translated action label for a hint with an action, in German too", async () => {
    await TestBed.inject(I18n).setLanguage("de");
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
      expect(byText(fixture, "z-panel", "Installierte JDKs")).toBeDefined();
    });
    TestBed.inject(ChecksStore).results.update((all) => ({
      ...all,
      "java.installed": {
        ...(all["java.installed"] as CheckSummary),
        hints: [{ text: "A newer patch version is available.", action: "java.update" }],
      },
    }));
    await settle(fixture);
    const java = byText(fixture, "z-panel", "Java");
    const hint = java?.querySelector(".hint");
    expect(hint?.querySelector("button")?.textContent).toContain("Aktualisieren und aufräumen");
    // The English text of the plugin script is not shown next to it.
    expect(hint?.textContent).not.toContain("newer patch version");
    // Labels come from the plugin translation: check label and plugin description.
    expect(java?.textContent).toContain("Installierte JDKs");
    expect(java?.textContent).toContain("JDKs installieren, wechseln und aktuell halten");
  });

  it("shows the text of a hint when it has no action, or its action is unknown", async () => {
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    TestBed.inject(ChecksStore).results.update((all) => ({
      ...all,
      "java.installed": {
        ...(all["java.installed"] as CheckSummary),
        hints: [
          { text: "Only a hint." },
          { text: "Needs a missing action.", action: "nothing.here" },
        ],
      },
    }));
    await settle(fixture);
    const hints = queryAll(byText(fixture, "z-panel", "Java") as HTMLElement, ".hint");
    expect(hints.map((h) => h.textContent?.trim())).toEqual([
      "Only a hint.",
      "Needs a missing action.",
    ]);
    expect(hints.some((h) => h.querySelector("button"))).toBe(false);
  });

  it("writes the home directory as ~ in summaries and wraps long paths", async () => {
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    TestBed.inject(ChecksStore).results.update((all) => ({
      ...all,
      "java.installed": {
        ...(all["java.installed"] as CheckSummary),
        summary: "JAVA_HOME not set in /home/dev/a/very/long/path/that/never/fits/.profile",
      },
    }));
    await settle(fixture);
    const summary = byText(fixture, "z-panel .summary", "JAVA_HOME");
    expect(summary?.textContent?.trim()).toBe(
      "JAVA_HOME not set in ~/a/very/long/path/that/never/fits/.profile",
    );
    // The wrapper breaks anywhere instead of pushing the tile wider.
    const wrapper = summary?.querySelector("dbx-wrap-text") as HTMLElement;
    expect(wrapper).not.toBeNull();
    const rule = [...document.querySelectorAll("style")]
      .map((style) => style.textContent ?? "")
      .find((css) => css.includes("overflow-wrap") && css.includes("anywhere"));
    expect(rule).toBeDefined();
  });

  it("asks the core for the checks in the language of the UI and again after a switch", async () => {
    const api = TestBed.inject(ApiClient);
    const checks = vi.spyOn(api, "checks");
    const fixture = TestBed.createComponent(OverviewPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-panel").length).toBe(4);
    });
    expect(checks).toHaveBeenLastCalledWith("en");
    await TestBed.inject(I18n).setLanguage("de");
    await vi.waitFor(() => {
      expect(checks).toHaveBeenLastCalledWith("de");
    });
  });
});
