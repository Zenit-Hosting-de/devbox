import { TestBed } from "@angular/core/testing";
import { provideRouter } from "@angular/router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { routes } from "../app.routes";
import { I18n } from "../i18n/i18n";
import type { MockBackend } from "../mock/mock-backend";
import { InfoStore } from "../stores/info.store";
import { DockStore } from "../stores/dock.store";
import { SessionsStore } from "../stores/sessions.store";
import { byText, queryAll, setUpApp, settle } from "../testing/test-app";
import { App } from "./app";

describe("App shell (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp([provideRouter(routes)]);
  });

  it("has a skip link first, the five areas in the sidebar, the dock and a live region", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-sidebar a[zsidebaritem], z-sidebar .z-side__item")).toHaveLength(
        5,
      );
    });
    const root = fixture.nativeElement as HTMLElement;
    expect(root.firstElementChild?.matches("a.z-skip-link")).toBe(true);
    expect(root.firstElementChild?.getAttribute("href")).toBe("#inhalt");
    expect(root.querySelector("main#inhalt")?.getAttribute("tabindex")).toBe("-1");
    const items = queryAll(fixture, "z-sidebar .z-side__item");
    ["Overview", "Plugins", "History", "Settings", "Credentials"].forEach((label, index) => {
      expect(items[index]?.textContent).toContain(label);
    });
    expect(
      queryAll<HTMLAnchorElement>(fixture, "z-sidebar .z-side__item").map((a) =>
        a.getAttribute("href"),
      ),
    ).toEqual(["/", "/plugins", "/verlauf", "/einstellungen", "/zugaenge"]);
    expect(root.querySelector("dbx-dock")).not.toBeNull();
    expect(root.querySelector("z-toast-outlet")).not.toBeNull();
    expect(root.querySelector("#announcer")).not.toBeNull();
    expect(root.querySelector("z-app-header")?.textContent).toContain("DevBox");
  });

  it("shows a banner instead of the offline notice while the core restarts", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture);
    await vi.waitFor(() => {
      expect(TestBed.inject(SessionsStore).status()).toBe("open");
    });
    expect(byText(fixture, "z-alert", "restarting")).toBeUndefined();

    backend.announceRestart();
    await settle(fixture, () => {
      expect(byText(fixture, "z-alert", "DevBox is restarting with the new version")).toBeDefined();
    });
    expect(byText(fixture, "z-alert", "No connection to the core")).toBeUndefined();
    expect(byText(fixture, "z-alert", "The connection is restored automatically")).toBeDefined();
  });

  it("connects exactly one socket after bootstrap", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture);
    await vi.waitFor(() => {
      expect(TestBed.inject(SessionsStore).status()).toBe("open");
    });
  });

  it("switches the language without a reload and sets the lang attribute", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-sidebar .z-side__item")).toHaveLength(5);
    });
    const select = fixture.nativeElement.querySelector("z-app-header select") as HTMLSelectElement;
    select.value = "de";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await vi.waitFor(() => {
      expect(TestBed.inject(I18n).language()).toBe("de");
    });
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-sidebar .z-side__item")[0]?.textContent).toContain("Übersicht");
    });
    expect(document.documentElement.lang).toBe("de");
    expect(fixture.nativeElement.querySelector("a.z-skip-link")?.textContent).toContain(
      "Zum Hauptinhalt springen",
    );
  });

  it("keeps the header and the language select in place when the language changes", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-sidebar .z-side__item")).toHaveLength(5);
    });
    const header = fixture.nativeElement.querySelector("z-app-header") as HTMLElement;
    const select = header.querySelector("select") as HTMLSelectElement;
    select.focus();
    select.value = "de";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await settle(fixture, () => {
      expect(queryAll(fixture, "z-sidebar .z-side__item")[0]?.textContent).toContain("Übersicht");
    });
    expect(fixture.nativeElement.querySelector("z-app-header")).toBe(header);
    expect(header.querySelector("select")).toBe(select);
    expect(document.activeElement).toBe(select);
    expect(header.querySelector("nav")?.getAttribute("aria-label")).toBe("Hauptnavigation");
  });

  it("does not open the socket when the core announces no WebSocket", async () => {
    const info = backend.info();
    vi.spyOn(backend, "info").mockReturnValue({ ...info, features: ["ui"] });
    const fixture = TestBed.createComponent(App);
    await settle(fixture);
    await vi.waitFor(() => {
      expect(TestBed.inject(InfoStore).info()).not.toBeNull();
    });
    await settle(fixture);
    expect(TestBed.inject(SessionsStore).status()).toBe("idle");
    expect(fixture.nativeElement.textContent).not.toContain("No connection to the core");
  });

  it("shows an offline banner while the connection is down", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture);
    await vi.waitFor(() => {
      expect(TestBed.inject(SessionsStore).status()).toBe("open");
    });
    TestBed.inject(SessionsStore).status.set("offline");
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("No connection to the core");
    TestBed.inject(SessionsStore).status.set("open");
    await settle(fixture);
    expect(fixture.nativeElement.textContent).not.toContain("No connection to the core");
  });

  it("announces the start and the end of a session in the live region", async () => {
    const fixture = TestBed.createComponent(App);
    await settle(fixture);
    backend.stepMs = 150;
    const sessions = TestBed.inject(SessionsStore);
    const region = (): string =>
      (fixture.nativeElement.querySelector("#announcer") as HTMLElement).textContent ?? "";
    const session = await sessions.create({ action: "java.update", dryRun: false });
    TestBed.inject(DockStore).openNew(session);
    await settle(fixture, () => {
      expect(region()).toContain("started");
    });
    await settle(fixture, () => {
      expect(region()).toContain("finished: Succeeded");
    });
    expect(byText(fixture, "[role=tab]", "Succeeded")).toBeDefined();
  });
});
