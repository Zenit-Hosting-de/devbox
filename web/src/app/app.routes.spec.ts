import { TestBed } from "@angular/core/testing";
import { provideRouter, Router, withComponentInputBinding } from "@angular/router";
import { RouterTestingHarness } from "@angular/router/testing";
import { beforeEach, describe, expect, it } from "vitest";
import { routes } from "./app.routes";
import { setUpApp } from "./testing/test-app";

describe("routes", () => {
  beforeEach(() => {
    setUpApp([provideRouter(routes, withComponentInputBinding())]);
  });

  async function open(url: string): Promise<{ url: string; text: string }> {
    const harness = await RouterTestingHarness.create();
    await harness.navigateByUrl(url);
    harness.detectChanges();
    return {
      url: TestBed.inject(Router).url,
      text: (harness.routeNativeElement as HTMLElement | null)?.textContent ?? "",
    };
  }

  it.each([
    ["/history", "/verlauf"],
    ["/history/s_0123456789", "/verlauf/s_0123456789"],
    ["/settings", "/einstellungen"],
    ["/credentials", "/zugaenge"],
  ])("redirects the English path %s to %s", async (english, german) => {
    const { url, text } = await open(english);
    expect(url).toBe(german);
    expect(text).not.toContain("Page not found");
  });

  it.each([
    "/",
    "/plugins",
    "/plugins/sources",
    "/plugins/java",
    "/verlauf",
    "/einstellungen",
    "/zugaenge",
  ])("opens %s without a redirect", async (path) => {
    const { url, text } = await open(path);
    expect(url).toBe(path);
    expect(text).not.toContain("Page not found");
  });

  it("still shows the not-found page for an unknown address", async () => {
    const { url, text } = await open("/nothing-here");
    expect(url).toBe("/nothing-here");
    expect(text).toContain("Page not found");
  });
});
