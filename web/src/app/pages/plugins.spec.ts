import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it } from "vitest";
import { MockBackend } from "../mock/mock-backend";
import { byText, queryAll, setUpApp, settle } from "../testing/test-app";
import { PluginsPage } from "./plugins";

describe("PluginsPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  async function open() {
    const fixture = TestBed.createComponent(PluginsPage);
    await settle(fixture, () => {
      expect(queryAll(fixture, "a[zRow], a.z-row").length).toBeGreaterThan(0);
    });
    return fixture;
  }

  it("has the tabs Installed and Sources, Installed being current", async () => {
    const fixture = await open();
    const tabs = queryAll(fixture, "nav a.z-tab");
    expect(tabs.map((t) => t.textContent?.trim())).toEqual(["Installed", "Sources"]);
    expect(tabs[0]?.getAttribute("aria-current")).toBe("page");
    expect(tabs[1]?.getAttribute("href")).toBe("/plugins/sources");
  });

  it("shows where every plugin comes from", async () => {
    backend.createRepository({ name: "demo-src", source: "file:///tmp/demo", trust: "prompt" });
    backend.approveRepository("demo-src", MockBackend.remoteRevision);
    const fixture = await open();
    const row = (name: string) => byText(fixture, "a.z-row", name)?.textContent ?? "";
    expect(row("Java")).toContain("Bundled");
    // A plugin of a source shows the name of the source.
    expect(row("Demo")).toContain("demo-src");
  });

  it("keeps disabled plugins in the list with their state", async () => {
    backend.patchPlugin("java", false);
    const fixture = await open();
    expect(byText(fixture, "a.z-row", "Java")?.textContent).toContain("Disabled");
  });
});
