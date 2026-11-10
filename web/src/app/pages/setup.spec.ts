import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ActionLauncher } from "../actions/action-launcher";
import type { MockBackend } from "../mock/mock-backend";
import { byText, query, queryAll, setUpApp, settle } from "../testing/test-app";
import { SetupPage } from "./setup";

async function open() {
  const fixture = TestBed.createComponent(SetupPage);
  await settle(fixture, () => {
    expect(queryAll(fixture, "[data-step]").length).toBeGreaterThan(0);
  });
  return fixture;
}

describe("SetupPage (against the mock backend)", () => {
  let backend: MockBackend;

  beforeEach(() => {
    backend = setUpApp();
  });

  it("shows the plan with the state of every step and where it comes from", async () => {
    const fixture = await open();
    const text = (ref: string) =>
      (query(fixture, `[data-step="${ref}"]`) as HTMLElement).textContent ?? "";
    expect(text("java.install")).toContain("Open");
    expect(text("java.install")).toContain("Install Java");
    expect(text("credentials:firmen-registry")).toContain("The secret is missing");
    expect(text("gone.x")).toContain("Blocked");
    expect(text("gone.x")).toContain("no such plugin");
    expect(fixture.nativeElement.textContent).toContain("Plan from: Profile example-team.");
    expect(fixture.nativeElement.textContent).toContain("0 of 4 steps done.");
  });

  it("starts the first open step and only offers a run button for open action steps", async () => {
    const fixture = await open();
    const start = vi.spyOn(TestBed.inject(ActionLauncher), "start").mockResolvedValue(undefined);
    (byText(fixture, "button", "Run the next step") as HTMLButtonElement).click();
    expect(start).toHaveBeenCalledWith("java.install");
    const buttons = (ref: string) =>
      queryAll(query(fixture, `[data-step="${ref}"]`) as HTMLElement, "button").length;
    expect(buttons("node.install")).toBe(1);
    expect(buttons("credentials:firmen-registry")).toBe(0);
    expect(buttons("gone.x")).toBe(0);
  });

  it("marks a step as done after its action ran and moves on to the next", async () => {
    const fixture = await open();
    backend.finishAction("java.install");
    await fixture.componentInstance.load();
    await settle(fixture);
    expect((query(fixture, '[data-step="java.install"]') as HTMLElement).textContent).toContain(
      "Done",
    );
    const start = vi.spyOn(TestBed.inject(ActionLauncher), "start").mockResolvedValue(undefined);
    (byText(fixture, "button", "Run the next step") as HTMLButtonElement).click();
    expect(start).toHaveBeenCalledWith("node.install");
  });

  it("lists the groups with a button per action", async () => {
    const fixture = await open();
    const group = query(fixture, '[data-group="update-all"]') as HTMLElement;
    const start = vi.spyOn(TestBed.inject(ActionLauncher), "start").mockResolvedValue(undefined);
    (byText(group, "button", "node.update") as HTMLButtonElement).click();
    expect(start).toHaveBeenCalledWith("node.update");
  });
});
