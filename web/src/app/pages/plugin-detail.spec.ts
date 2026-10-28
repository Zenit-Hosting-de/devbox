import { Component } from "@angular/core";
import { TestBed, type ComponentFixture } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InfoStore } from "../stores/info.store";
import { MockBackend, MockProblem } from "../mock/mock-backend";
import { Dock } from "../shell/dock";
import { DockStore } from "../stores/dock.store";
import { SessionsStore } from "../stores/sessions.store";
import { byText, fakeTerminals, query, queryAll, setUpApp, settle } from "../testing/test-app";
import { PluginDetailPage } from "./plugin-detail";

@Component({
  imports: [PluginDetailPage, Dock],
  template: `<dbx-plugin-detail-page [id]="id" /><dbx-dock />`,
})
class Host {
  id = "java";
}

function dialog(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>(".z-dialog");
}

function dialogButton(text: string): HTMLButtonElement | undefined {
  return [...(dialog()?.querySelectorAll("button") ?? [])].find((b) =>
    (b.textContent ?? "").includes(text),
  );
}

async function open(): Promise<ComponentFixture<Host>> {
  // The shell connects the socket after bootstrap; the test does it itself.
  TestBed.inject(SessionsStore).connect();
  const fixture = TestBed.createComponent(Host);
  await settle(fixture, () => {
    expect(fixture.nativeElement.querySelector("article.check")).not.toBeNull();
    expect(fixture.nativeElement.querySelector("dbx-settings-group")).not.toBeNull();
  });
  return fixture;
}

describe("PluginDetailPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  it("shows the checks with badge, summary, details and hints, the actions and the settings", async () => {
    const fixture = await open();
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain("Java");
    expect(text).toContain("Version 1.0.0 · Ready");
    // Checks: translated label comes from the manifest in English; badge and summary from the result.
    expect(text).toContain("Installed JDKs");
    expect(text).toContain("2 JDKs installed, default 21");
    expect(text).toContain("Install location");
    expect(text).toContain("/home/dev/.local/share/jdk");
    // A hint with an action shows the (translated) label of the action, not the hint text.
    expect(text).not.toContain("A newer patch version of JDK 21 is available.");
    expect(byText(fixture, ".hint button", "Update and clean up")).toBeDefined();
    // Actions as buttons; the dangerous one is styled as danger.
    expect(byText(fixture, "button", "Install JDK")).toBeDefined();
    expect(byText(fixture, "button", "Remove JDK")?.classList.contains("z-btn--danger")).toBe(true);
    // The settings of the plugin, via the shared group component.
    expect(queryAll(fixture, "dbx-settings-group dbx-schema-form")).toHaveLength(1);
    expect(text).toContain("Release API");
  });

  it("shows check details with ~ for the home directory and the full path as tooltip", async () => {
    await TestBed.inject(InfoStore).load();
    const fixture = await open();
    const value = [...fixture.nativeElement.querySelectorAll("dd dbx-wrap-text span")].find((el) =>
      (el as HTMLElement).textContent?.includes("~/.local/share/jdk"),
    ) as HTMLElement | undefined;
    expect(value).toBeDefined();
    expect(fixture.nativeElement.textContent).not.toContain("/home/dev/.local/share/jdk");
    expect(value?.getAttribute("tabindex")).toBe("0");
    value?.focus();
    await settle(fixture, () => {
      expect(document.querySelector(".cdk-overlay-container")?.textContent ?? "").toContain(
        "/home/dev/.local/share/jdk",
      );
    });
  });

  it("refreshes only the checks of this plugin", async () => {
    const fixture = await open();
    const refresh = vi.spyOn(backend, "refreshChecks");
    byText(fixture, "button", "Refresh checks")?.click();
    await settle(fixture);
    expect(refresh).toHaveBeenCalledWith(["java.installed", "java.latest"]);
  });

  it("starts an action from the dialog prefilled from the resolved defaults and opens a dock tab with output", async () => {
    const fixture = await open();
    const create = vi.spyOn(backend, "createSession");

    byText(fixture, "button", "Install JDK")?.click();
    await settle(fixture, () => {
      expect(dialog()).not.toBeNull();
    });
    // Prefilled from resolvedDefaults: version 21 pressed, "make default" on.
    const pressed = [...(dialog()?.querySelectorAll("z-segment button") ?? [])].filter(
      (b) => b.getAttribute("aria-pressed") === "true",
    );
    expect(pressed.map((b) => b.textContent?.trim())).toEqual(["21"]);
    expect(dialog()?.querySelector<HTMLInputElement>("input[role=switch]")?.checked).toBe(true);

    dialogButton("Start")?.click();
    await settle(fixture, () => {
      expect(queryAll(fixture, "[role=tab]")).toHaveLength(1);
      expect(fakeTerminals[0]?.text).toContain("Install JDK");
    });
    expect(create).toHaveBeenCalledTimes(1);
    expect(create.mock.calls[0]?.[0]).toMatchObject({
      action: "java.install",
      params: { version: 21, makeDefault: true },
      dryRun: false,
      cols: 120,
      rows: 32,
    });
    // The dialog is gone, the tab shows the state and the terminal received coloured output.
    expect(dialog()).toBeNull();
    expect(fakeTerminals[0]?.text).toContain("\u001b[1;34m==> Install JDK");
    await settle(fixture, () => {
      expect(fixture.nativeElement.querySelector("[role=tab]")?.textContent).toContain("Succeeded");
    });
    expect(fakeTerminals[0]?.text).toContain("done");
  });

  it("sends a dry run when the switch is on", async () => {
    const fixture = await open();
    const create = vi.spyOn(backend, "createSession");
    // The hint of the check offers the same action; the one in the actions panel opens the dialog.
    queryAll(fixture, "button")
      .filter((b) => (b.textContent ?? "").includes("Update and clean up"))
      .at(-1)
      ?.click();
    await settle(fixture, () => {
      expect(dialog()).not.toBeNull();
    });
    dialog()?.querySelector<HTMLInputElement>("input[role=switch]")?.click();
    await settle(fixture);
    expect(dialogButton("Start dry run")).toBeDefined();
    dialogButton("Start dry run")?.click();
    await settle(fixture, () => {
      expect(create).toHaveBeenCalled();
    });
    expect(create.mock.calls[0]?.[0]).toMatchObject({ action: "java.update", dryRun: true });
    expect(create.mock.calls[0]?.[0]).not.toHaveProperty("yes");
  });

  it("asks for an explicit confirmation and sends yes only after it", async () => {
    const fixture = await open();
    const create = vi.spyOn(backend, "createSession");

    byText(fixture, "button", "Remove JDK")?.click();
    await settle(fixture, () => {
      expect(dialog()).not.toBeNull();
    });
    // The (manifest) confirmation text is shown.
    expect(dialog()?.textContent).toContain("The JDK is deleted.");
    const select = dialog()?.querySelector<HTMLSelectElement>("select") as HTMLSelectElement;
    expect([...select.options].map((o) => o.value)).toEqual(["", "21.0.5", "17.0.13"]);
    select.value = "17.0.13";
    select.dispatchEvent(new Event("change", { bubbles: true }));

    // Not confirmed: nothing is sent.
    dialogButton("Start")?.click();
    await settle(fixture);
    expect(create).not.toHaveBeenCalled();
    expect(dialog()?.textContent).toContain("has to be confirmed explicitly");

    // Confirmed: the request carries yes: true.
    dialog()?.querySelector<HTMLInputElement>("input[type=checkbox]:not([role=switch])")?.click();
    await settle(fixture);
    dialogButton("Start")?.click();
    await settle(fixture, () => {
      expect(create).toHaveBeenCalledTimes(1);
    });
    expect(create.mock.calls[0]?.[0]).toMatchObject({
      action: "java.remove",
      params: { version: "17.0.13" },
      yes: true,
    });
    expect(TestBed.inject(DockStore).tabs()).toHaveLength(1);
  });

  it("maps 422 errors by pointer /params/<name> onto the field", async () => {
    const fixture = await open();
    vi.spyOn(backend, "createSession").mockImplementation(() => {
      throw new MockProblem(422, "validation", "The request is invalid.", [
        { pointer: "/params/version", code: "enum", message: "not installable" },
      ]);
    });
    byText(fixture, "button", "Install JDK")?.click();
    await settle(fixture, () => {
      expect(dialog()).not.toBeNull();
    });
    dialogButton("Start")?.click();
    await settle(fixture, () => {
      expect(dialog()?.querySelector(".z-field__error")).not.toBeNull();
    });
    const field = dialog()?.querySelector("z-field");
    expect(field?.textContent).toContain("The value is not one of the allowed values.");
    // The dialog stays open and no tab was created.
    expect(dialog()).not.toBeNull();
    expect(TestBed.inject(DockStore).tabs()).toHaveLength(0);
  });

  it("shows a general error for a failure without field pointers", async () => {
    const fixture = await open();
    vi.spyOn(backend, "createSession").mockImplementation(() => {
      throw new MockProblem(409, "conflict", "java.install is already running.");
    });
    byText(fixture, "button", "Install JDK")?.click();
    await settle(fixture, () => {
      expect(dialog()).not.toBeNull();
    });
    dialogButton("Start")?.click();
    await settle(fixture, () => {
      expect(dialog()?.querySelector("z-alert")).not.toBeNull();
    });
    expect(dialog()?.textContent).toContain("already running");
  });

  it("shows the unknown-plugin state for a missing plugin", async () => {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.id = "nope";
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("Plugin not found");
    });
    expect(query(fixture, "article.check")).toBeNull();
  });

  it("does not offer actions for a plugin that is not ready", async () => {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.id = "docker";
    await settle(fixture, () => {
      expect(fixture.nativeElement.textContent).toContain("Docker");
    });
    expect(fixture.nativeElement.textContent).toContain("Plugin is not ready: Blocked");
    expect(fixture.nativeElement.textContent).toContain(
      "Actions are only available for ready plugins.",
    );
    expect(byText(fixture, "button", "Install Docker")).toBeUndefined();
  });
});
