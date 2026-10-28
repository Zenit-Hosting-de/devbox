import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import { ApiProblem } from "../api/problem";
import { I18n } from "../i18n/i18n";
import {
  byText,
  query,
  queryAll,
  setUpApp,
  settle,
  typeInto,
  type Root,
} from "../testing/test-app";
import { SettingsPage } from "./settings";

function panel(fixture: Root, title: string): HTMLElement {
  const found = queryAll(fixture, "z-panel").find((p) =>
    (p.querySelector(".z-panel__title")?.textContent ?? "").trim().startsWith(title),
  );
  if (!found) {
    throw new Error(`panel ${title} not found`);
  }
  return found;
}

function inputIn(root: HTMLElement, name: string): HTMLInputElement {
  const input = root.querySelector<HTMLInputElement>(`#set-${name}`);
  if (!input) {
    throw new Error(`input ${name} not found`);
  }
  return input;
}

async function open() {
  const fixture = TestBed.createComponent(SettingsPage);
  await settle(fixture, () => {
    expect(queryAll(fixture, "dbx-settings-group").length).toBeGreaterThan(0);
  });
  return fixture;
}

describe("SettingsPage (against the mock backend)", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("groups the settings by plugin with the origin of every value", async () => {
    const fixture = await open();
    const titles = queryAll(fixture, "z-panel .z-panel__title").map((t) => t.textContent?.trim());
    expect(titles).toEqual(["Docker", "Java", "Node.js", "System", "Appearance"]);
    const java = panel(fixture, "Java");
    const badges = queryAll(java, "z-badge").map((b) => b.textContent?.trim());
    // Default of the plugin, user value, profile lock.
    expect(badges).toContain("Default");
    expect(badges).toContain("User");
    expect(badges).toContain("Profile example-team");
  });

  it("shows a locked field read-only and names the profile", async () => {
    const fixture = await open();
    const java = panel(fixture, "Java");
    const keep = inputIn(java, "java-keepPatches");
    expect(keep.readOnly).toBe(true);
    expect(keep.value).toBe("2");
    expect(java.textContent).toContain("Locked by profile example-team");
    // A locked field offers no reset, an editable one does not set readonly.
    expect(inputIn(java, "java-vendorApi").readOnly).toBe(false);
  });

  it("saves only the changed keys and reloads the values", async () => {
    const fixture = await open();
    const api = TestBed.inject(ApiClient);
    const patch = vi.spyOn(api, "patchSettings");
    const java = panel(fixture, "Java");
    const save = byText(java, "button", "Save") as HTMLButtonElement;
    expect(save.disabled).toBe(true);

    typeInto(inputIn(java, "java-vendorApi"), "https://mirror.example.com/v3");
    await settle(fixture);
    expect(save.disabled).toBe(false);
    expect(java.textContent).toContain("1 change");
    // The Node.js group is not affected by edits in the Java group.
    expect(
      (byText(panel(fixture, "Node.js"), "button", "Save") as HTMLButtonElement).disabled,
    ).toBe(true);

    save.click();
    await settle(fixture, () => {
      expect(patch).toHaveBeenCalledTimes(1);
    });
    expect(patch).toHaveBeenCalledWith({ "java.vendorApi": "https://mirror.example.com/v3" });
    // Untouched keys are not part of the request.
    expect(Object.keys(patch.mock.calls[0]?.[0] ?? {})).toEqual(["java.vendorApi"]);

    // After the reload the value comes from the user layer.
    await settle(fixture, () => {
      expect(
        queryAll(panel(fixture, "Java"), "z-badge").filter((b) => b.textContent?.trim() === "User"),
      ).toHaveLength(2);
    });
    expect(inputIn(panel(fixture, "Java"), "java-vendorApi").value).toBe(
      "https://mirror.example.com/v3",
    );
    expect(save.disabled).toBe(true);
  });

  it("resets only values that come from the user layer (PATCH with null)", async () => {
    const fixture = await open();
    const patch = vi.spyOn(TestBed.inject(ApiClient), "patchSettings");
    const java = panel(fixture, "Java");
    const resetButtons = queryAll(java, "button").filter((b) => b.textContent?.includes("Reset"));
    // defaultVersion has a user value; vendorApi (plugin default) and keepPatches (locked) have none.
    expect(resetButtons).toHaveLength(1);
    resetButtons[0]?.click();
    await settle(fixture, () => {
      expect(patch).toHaveBeenCalledWith({ "java.defaultVersion": null });
    });
    await settle(fixture, () => {
      expect(
        queryAll(panel(fixture, "Java"), "button").filter((b) => b.textContent?.includes("Reset")),
      ).toHaveLength(0);
    });
  });

  it("maps 422 errors by pointer /<key> onto the field", async () => {
    const fixture = await open();
    vi.spyOn(TestBed.inject(ApiClient), "patchSettings").mockRejectedValueOnce(
      new ApiProblem({
        code: "validation",
        status: 422,
        title: "validation",
        errors: [{ pointer: "/node.distUrl", code: "uri" }],
      }),
    );
    const node = panel(fixture, "Node.js");
    typeInto(inputIn(node, "node-distUrl"), "https://other.example.com/dist");
    await settle(fixture);
    (byText(node, "button", "Save") as HTMLButtonElement).click();
    await settle(fixture, () => {
      expect(panel(fixture, "Node.js").textContent).toContain("A valid address is required.");
    });
    expect(inputIn(panel(fixture, "Node.js"), "node-distUrl").getAttribute("aria-invalid")).toBe(
      "true",
    );
  });

  it("shows a setting-locked conflict at the field", async () => {
    const fixture = await open();
    vi.spyOn(TestBed.inject(ApiClient), "patchSettings").mockRejectedValueOnce(
      new ApiProblem({ code: "setting-locked", status: 409, detail: "locked by profile" }),
    );
    const node = panel(fixture, "Node.js");
    typeInto(inputIn(node, "node-distUrl"), "https://other.example.com/dist");
    await settle(fixture);
    (byText(node, "button", "Save") as HTMLButtonElement).click();
    await settle(fixture, () => {
      expect(panel(fixture, "Node.js").textContent).toContain("The value is locked.");
    });
  });

  it("validates on the client before it sends anything", async () => {
    const fixture = await open();
    const patch = vi.spyOn(TestBed.inject(ApiClient), "patchSettings");
    const node = panel(fixture, "Node.js");
    typeInto(inputIn(node, "node-distUrl"), "not a url");
    await settle(fixture);
    (byText(node, "button", "Save") as HTMLButtonElement).click();
    await settle(fixture);
    expect(patch).not.toHaveBeenCalled();
    expect(panel(fixture, "Node.js").textContent).toContain("A valid address is required.");
  });

  it("filters by key and label and keeps edits of hidden fields", async () => {
    const fixture = await open();
    const java = panel(fixture, "Java");
    typeInto(inputIn(java, "java-vendorApi"), "https://mirror.example.com/v3");
    const search = query<HTMLInputElement>(fixture, "#settings-search") as HTMLInputElement;
    typeInto(search, "keepPatches");
    await settle(fixture);
    expect(queryAll(fixture, "z-panel .z-panel__title").map((t) => t.textContent?.trim())).toEqual([
      "Java",
      "Appearance",
    ]);
    expect(panel(fixture, "Java").querySelector("#set-java-vendorApi")).toBeNull();
    expect(panel(fixture, "Java").querySelector("#set-java-keepPatches")).not.toBeNull();

    // Search by label.
    typeInto(search, "release api");
    await settle(fixture);
    expect(panel(fixture, "Java").querySelector("#set-java-vendorApi")).not.toBeNull();
    // The edit survived the filter.
    expect(inputIn(panel(fixture, "Java"), "java-vendorApi").value).toBe(
      "https://mirror.example.com/v3",
    );

    typeInto(search, "zzzz");
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("No matching setting");
  });

  it("switches language and colour scheme without a reload", async () => {
    const fixture = await open();
    const i18n = TestBed.inject(I18n);
    expect(fixture.nativeElement.textContent).toContain("Appearance");
    byText(fixture, "dbx-appearance z-segment button", "Deutsch")?.click();
    await vi.waitFor(() => {
      expect(i18n.language()).toBe("de");
    });
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("Erscheinungsbild");
    expect(fixture.nativeElement.querySelector("h1")?.textContent).toContain("Einstellungen");
    expect(document.documentElement.lang).toBe("de");
  });
});
