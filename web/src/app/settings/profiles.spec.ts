import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/api-client";
import { I18n } from "../i18n/i18n";
import { byText, query, queryAll, setUpApp, settle, typeInto } from "../testing/test-app";
import { Profiles, validateProfile } from "./profiles";

function dialogButton(text: string): HTMLButtonElement | undefined {
  return [...document.body.querySelectorAll<HTMLButtonElement>(".z-dialog button")].find((b) =>
    (b.textContent ?? "").includes(text),
  );
}

async function open() {
  const fixture = TestBed.createComponent(Profiles);
  await settle(fixture, () => {
    expect(queryAll(fixture, "[data-profile]").length).toBeGreaterThan(0);
  });
  return fixture;
}

describe("Profiles (against the mock backend)", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("lists a profile with origin, source and locks", async () => {
    const fixture = await open();
    const entry = query(fixture, '[data-profile="example-team"]') as HTMLElement;
    expect(entry.textContent).toContain("Example team (example-team)");
    expect(entry.textContent).toContain("User");
    expect(entry.textContent).toContain("git+https://git.example.com/platform/devbox-profile.git");
    expect(entry.textContent).toContain("java.keepPatches");
    expect(entry.textContent).toContain("3 settings, 1 locked");
  });

  it("checks the form before it sends and adds a profile", async () => {
    const fixture = await open();
    const api = TestBed.inject(ApiClient);
    const create = vi.spyOn(api, "createProfile");
    (byText(fixture, "button", "Add profile") as HTMLButtonElement).click();
    await settle(fixture);

    typeInto(query(fixture, "#profile-source") as HTMLInputElement, "ftp://example.com/p.json");
    typeInto(query(fixture, "#profile-issuer") as HTMLInputElement, "https://issuer.example.com");
    (byText(fixture, "button", "Fetch and add") as HTMLButtonElement).click();
    await settle(fixture);
    expect(create).not.toHaveBeenCalled();
    expect(fixture.nativeElement.textContent).toContain("Use file://, https://");
    expect(fixture.nativeElement.textContent).toContain("Issuer and identity go together.");

    typeInto(query(fixture, "#profile-source") as HTMLInputElement, "https://example.com/web.json");
    typeInto(query(fixture, "#profile-identity") as HTMLInputElement, "https://ci.example.com/x");
    (byText(fixture, "button", "Fetch and add") as HTMLButtonElement).click();
    await settle(fixture, () => {
      expect(query(fixture, '[data-profile="web"]')).not.toBeNull();
    });
    expect(create).toHaveBeenCalledWith({
      source: "https://example.com/web.json",
      trust: { issuer: "https://issuer.example.com", identity: "https://ci.example.com/x" },
    });
    expect(query(fixture, "#profile-form")).toBeNull();
    expect((query(fixture, '[data-profile="web"]') as HTMLElement).textContent).toContain(
      "Signature required",
    );
  });

  it("fetches again and removes after confirmation", async () => {
    const fixture = await open();
    const api = TestBed.inject(ApiClient);
    const update = vi.spyOn(api, "updateProfiles");
    (byText(fixture, "button", "Fetch again") as HTMLButtonElement).click();
    await settle(fixture, () => expect(update).toHaveBeenCalled());

    const entry = query(fixture, '[data-profile="example-team"]') as HTMLElement;
    (byText(entry, "button", "Remove") as HTMLButtonElement).click();
    await settle(fixture, () => expect(dialogButton("Remove")).toBeDefined());
    dialogButton("Remove")?.click();
    await settle(fixture, () => {
      expect(query(fixture, '[data-profile="example-team"]')).toBeNull();
    });
    expect(fixture.nativeElement.textContent).toContain("No profile");
  });

  it("validates sources, refs and trust", () => {
    const i18n = TestBed.inject(I18n);
    const v = (source: string, ref = "", issuer = "", identity = "") =>
      Object.keys(validateProfile(i18n, { source, ref, issuer, identity }));
    expect(v("")).toEqual(["source"]);
    expect(v("file:///etc/p.json")).toEqual([]);
    expect(v("https://example.com/p.json", "main")).toEqual(["ref"]);
    expect(v("git+ssh://git@example.com/p.git", "main")).toEqual([]);
    expect(v("git+https://example.com/p.git", "", "", "id")).toEqual(["issuer"]);
  });
});
