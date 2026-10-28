import { Component } from "@angular/core";
import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it } from "vitest";
import { InfoStore } from "../stores/info.store";
import { setUpApp, settle } from "../testing/test-app";
import { WrapText } from "./wrap-text";

@Component({
  selector: "dbx-test-host",
  imports: [WrapText],
  template: `<dbx-wrap-text [text]="text" />`,
})
class Host {
  text = "not set in /home/dev/.npmrc";
}

describe("WrapText", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("shows the home directory as ~ once the core told it", async () => {
    await TestBed.inject(InfoStore).load();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    expect(fixture.nativeElement.textContent.trim()).toBe("not set in ~/.npmrc");
  });

  it("shows the text as it is while the home is not known", async () => {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    expect(fixture.nativeElement.textContent.trim()).toBe("not set in /home/dev/.npmrc");
  });

  it("wraps anywhere, so a long path cannot widen its tile", async () => {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    const css = [...document.querySelectorAll("style")]
      .map((style) => style.textContent ?? "")
      .find((text) => text.includes("overflow-wrap"));
    expect(css).toContain("overflow-wrap: anywhere");
  });

  it("opens the full text as a tooltip when it was shortened", async () => {
    await TestBed.inject(InfoStore).load();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    const span = fixture.nativeElement.querySelector("span") as HTMLElement;
    span.dispatchEvent(new MouseEvent("mouseenter", { bubbles: true }));
    span.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
    span.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
    await settle(fixture);
    const tip = document.querySelector(".cdk-overlay-container")?.textContent ?? "";
    expect(tip).toContain("/home/dev/.npmrc");
  });
});
