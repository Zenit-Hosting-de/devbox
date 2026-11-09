import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import { byText, query, queryAll, setUpApp, settle, typeInto } from "../testing/test-app";
import { CatalogPage } from "./catalog";

function dialogButton(text: string): HTMLButtonElement | undefined {
  return [...document.body.querySelectorAll<HTMLButtonElement>(".z-dialog button")].find((b) =>
    (b.textContent ?? "").includes(text),
  );
}

async function open() {
  const fixture = TestBed.createComponent(CatalogPage);
  await settle(fixture, () => {
    expect(queryAll(fixture, "[data-hit]").length).toBeGreaterThan(0);
  });
  return fixture;
}

describe("CatalogPage (against the mock backend)", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("lists and searches the catalog", async () => {
    const fixture = await open();
    expect(queryAll(fixture, "[data-hit]").map((h) => h.getAttribute("data-hit"))).toEqual([
      "kubectl",
      "helm",
    ]);
    typeInto(query(fixture, "#catalog-search") as HTMLInputElement, "helm");
    (query(fixture, "form.search") as HTMLFormElement).dispatchEvent(new Event("submit"));
    await settle(fixture, () => {
      expect(queryAll(fixture, "[data-hit]").map((h) => h.getAttribute("data-hit"))).toEqual([
        "helm",
      ]);
    });
  });

  it("installs only after the permissions are confirmed, then removes", async () => {
    const fixture = await open();
    const api = TestBed.inject(ApiClient);
    const install = vi.spyOn(api, "installFromCatalog");
    const kubectl = () => query(fixture, '[data-hit="kubectl"]') as HTMLElement;

    (byText(kubectl(), "button", "Install") as HTMLButtonElement).click();
    await settle(fixture, () => expect(dialogButton("Install")).toBeDefined());
    expect(document.body.querySelector(".z-dialog")?.textContent).toContain(
      "Network: https://downloads.example.com",
    );
    expect(install).toHaveBeenCalledTimes(1);
    expect(install.mock.calls[0]?.[0].confirm).toBe(false);

    dialogButton("Install")?.click();
    await settle(fixture, () => expect(kubectl().textContent).toContain("installed 1.2.3"));
    expect(install).toHaveBeenCalledTimes(2);
    expect(install.mock.calls[1]?.[0].confirm).toBe(true);

    (byText(kubectl(), "button", "Remove") as HTMLButtonElement).click();
    await settle(fixture, () => expect(dialogButton("Remove")).toBeDefined());
    dialogButton("Remove")?.click();
    await settle(fixture, () => expect(kubectl().textContent).not.toContain("installed"));
  });

  it("installs nothing when the confirmation is cancelled", async () => {
    const fixture = await open();
    const api = TestBed.inject(ApiClient);
    const install = vi.spyOn(api, "installFromCatalog");
    const helm = query(fixture, '[data-hit="helm"]') as HTMLElement;
    (byText(helm, "button", "Install") as HTMLButtonElement).click();
    await settle(fixture, () => expect(dialogButton("Cancel")).toBeDefined());
    dialogButton("Cancel")?.click();
    await settle(fixture, () => expect(document.body.querySelector(".z-dialog")).toBeNull());
    expect(install).toHaveBeenCalledTimes(1);
    expect(helm.textContent).not.toContain("installed");
  });
});
