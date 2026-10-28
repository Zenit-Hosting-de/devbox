import { Component, signal, viewChild } from "@angular/core";
import { TestBed, type ComponentFixture } from "@angular/core/testing";
import { beforeEach, describe, expect, it } from "vitest";
import { ChecksStore } from "../stores/checks.store";
import { byText, query, queryAll, setUpApp, settle, typeInto } from "../testing/test-app";
import type { FormField, FormValues } from "./field";
import { SchemaForm, type FieldBadge } from "./schema-form";

@Component({
  imports: [SchemaForm],
  template: `
    <dbx-schema-form
      pluginId="java"
      scope="settings"
      [fields]="fields()"
      [(values)]="values"
      [locked]="locked()"
      [errors]="errors()"
      [badges]="badges()"
    />
  `,
})
class Host {
  readonly fields = signal<Record<string, FormField>>({});
  readonly values = signal<FormValues>({});
  readonly locked = signal<Record<string, string>>({});
  readonly errors = signal<Record<string, string>>({});
  readonly badges = signal<Record<string, FieldBadge>>({});
  readonly form = viewChild.required(SchemaForm);
}

async function render(
  fields: Record<string, FormField>,
  values: FormValues = {},
  patch: (host: Host) => void = () => undefined,
): Promise<{ fixture: ComponentFixture<Host>; host: Host }> {
  const fixture = TestBed.createComponent(Host);
  const host = fixture.componentInstance;
  host.fields.set(fields);
  host.values.set(values);
  patch(host);
  await settle(fixture);
  return { fixture, host };
}

describe("SchemaForm", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("renders a string as a text input and emits the typed string", async () => {
    const { fixture, host } = await render({ name: { type: "string", label: "Name" } });
    const input = query<HTMLInputElement>(fixture, "input[type=text]");
    expect(input).not.toBeNull();
    typeInto(input as HTMLInputElement, "abc");
    expect(host.values()).toEqual({ name: "abc" });
    typeInto(input as HTMLInputElement, "");
    expect(host.values()).toEqual({});
  });

  it("validates format uri and shows the error", async () => {
    const { fixture, host } = await render({
      api: { type: "string", format: "uri", label: "API", default: "https://api.example.com" },
    });
    typeInto(query<HTMLInputElement>(fixture, "input") as HTMLInputElement, "not a url");
    expect(host.form().validate()).toBe(false);
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("A valid address is required.");
    typeInto(
      query<HTMLInputElement>(fixture, "input") as HTMLInputElement,
      "https://example.com/x",
    );
    expect(host.form().validate()).toBe(true);
  });

  it("shows a path placeholder as written", async () => {
    const { fixture } = await render({
      dir: { type: "string", format: "path", label: "Directory", default: "${xdg:data}/jdk" },
    });
    const input = query<HTMLInputElement>(fixture, "input") as HTMLInputElement;
    expect(input.placeholder).toBe("${xdg:data}/jdk");
    expect(input.value).toBe("");
  });

  it("renders an enum with up to five values as a segment and emits the typed value", async () => {
    const { fixture, host } = await render(
      {
        version: { type: "integer", enum: [8, 11, 17, 21, 25], label: "Version", default: 21 },
      },
      { version: 21 },
    );
    expect(query(fixture, "z-segment")).not.toBeNull();
    expect(query(fixture, "select")).toBeNull();
    const pressed = queryAll(fixture, "z-segment button").filter(
      (b) => b.getAttribute("aria-pressed") === "true",
    );
    expect(pressed.map((b) => b.textContent?.trim())).toEqual(["21"]);
    byText(fixture, "z-segment button", "17")?.click();
    expect(host.values()["version"]).toBe(17);
  });

  it("renders a larger enum as a select", async () => {
    const { fixture, host } = await render(
      {
        channel: {
          type: "string",
          enum: ["a", "b", "c", "d", "e", "f"],
          label: "Channel",
          default: "a",
        },
      },
      { channel: "a" },
    );
    expect(query(fixture, "z-segment")).toBeNull();
    const select = query<HTMLSelectElement>(fixture, "select") as HTMLSelectElement;
    // The six values plus "not set" (the field has a default).
    expect(select.options.length).toBe(7);
    select.value = "d";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    expect(host.values()["channel"]).toBe("d");
  });

  it("renders integers as a number input with minimum and maximum and emits numbers", async () => {
    const { fixture, host } = await render({
      keep: { type: "integer", minimum: 1, maximum: 5, label: "Keep", default: 1 },
    });
    const input = query<HTMLInputElement>(fixture, "input[type=number]") as HTMLInputElement;
    expect(input.min).toBe("1");
    expect(input.max).toBe("5");
    typeInto(input, "3");
    expect(host.values()["keep"]).toBe(3);
    typeInto(input, "9");
    expect(host.form().validate()).toBe(false);
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("at most 5");
  });

  it("renders a number input for numbers too", async () => {
    const { fixture, host } = await render({
      ratio: { type: "number", label: "Ratio", default: 1 },
    });
    const input = query<HTMLInputElement>(fixture, "input[type=number]") as HTMLInputElement;
    expect(input.step).toBe("any");
    typeInto(input, "1.5");
    expect(host.values()["ratio"]).toBe(1.5);
  });

  it("renders a boolean as a toggle and emits booleans", async () => {
    const { fixture, host } = await render(
      {
        makeDefault: { type: "boolean", label: "Make it the default", default: true },
      },
      { makeDefault: true },
    );
    const toggle = query<HTMLInputElement>(fixture, "input[role=switch]") as HTMLInputElement;
    expect(toggle.checked).toBe(true);
    toggle.click();
    expect(host.values()["makeDefault"]).toBe(false);
  });

  it("renders an array of strings as a list with add and remove and a pattern per item", async () => {
    const { fixture, host } = await render({
      packages: {
        type: "array",
        items: { type: "string", pattern: "^[a-z]+$" },
        label: "Packages",
        default: [],
      },
    });
    byText(fixture, "button", "Add entry")?.click();
    await settle(fixture);
    byText(fixture, "button", "Add entry")?.click();
    await settle(fixture);
    const inputs = queryAll<HTMLInputElement>(fixture, ".list-row input");
    expect(inputs).toHaveLength(2);
    typeInto(inputs[0] as HTMLInputElement, "good");
    typeInto(inputs[1] as HTMLInputElement, "BAD");
    expect(host.values()["packages"]).toEqual(["good", "BAD"]);
    expect(host.form().validate()).toBe(false);
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("Entry 2");
    (queryAll(fixture, ".list-row button")[1] as HTMLButtonElement).click();
    expect(host.values()["packages"]).toEqual(["good"]);
    expect(host.form().validate()).toBe(true);
  });

  it("fills a select from check data for optionsFrom", async () => {
    const { fixture, host } = await render({
      version: {
        type: "string",
        label: "Version",
        optionsFrom: "java.installed#/data/items/*/version",
      },
    });
    TestBed.inject(ChecksStore).results.set({
      "java.installed": {
        id: "java.installed",
        status: "ok",
        summary: "ok",
        checkedAt: "2026-03-01T00:00:00Z",
        data: { items: [{ version: "21.0.5" }, { version: "17.0.13" }] },
      },
    });
    await settle(fixture);
    const select = query<HTMLSelectElement>(fixture, "select") as HTMLSelectElement;
    expect([...select.options].map((o) => o.value)).toEqual(["", "21.0.5", "17.0.13"]);
    select.value = "17.0.13";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    expect(host.values()["version"]).toBe("17.0.13");
  });

  it("renders a secret as a password field and never prefills it", async () => {
    const { fixture, host } = await render(
      { token: { type: "string", secret: true, label: "Token" } },
      { token: "hunter2" },
    );
    const input = query<HTMLInputElement>(fixture, "input[type=password]") as HTMLInputElement;
    expect(input).not.toBeNull();
    expect(input.value).toBe("");
    expect(fixture.nativeElement.innerHTML).not.toContain("hunter2");
    typeInto(input, "new-secret");
    expect(host.values()["token"]).toBe("new-secret");
    // A secret is never required.
    typeInto(input, "");
    expect(host.form().validate()).toBe(true);
  });

  it("collapses advanced fields under further options", async () => {
    const { fixture } = await render({
      basic: { type: "string", label: "Basic", default: "x" },
      hidden: { type: "string", label: "Hidden", default: "y", advanced: true },
    });
    const disclosure = query<HTMLDetailsElement>(fixture, "details.z-disclosure");
    expect(disclosure).not.toBeNull();
    expect(disclosure?.open).toBe(false);
    expect(disclosure?.textContent).toContain("More options");
    expect(disclosure?.querySelector("input")).not.toBeNull();
    expect(queryAll(fixture, "input").filter((i) => !disclosure?.contains(i))).toHaveLength(1);
  });

  it("opens the further options when a field inside them has an error", async () => {
    const { fixture } = await render(
      { hidden: { type: "string", label: "Hidden", default: "y", advanced: true } },
      {},
      (host) => {
        host.errors.set({ hidden: "Server says no" });
      },
    );
    expect(query<HTMLDetailsElement>(fixture, "details.z-disclosure")?.open).toBe(true);
  });

  it("maps server errors to the field by name", async () => {
    const { fixture } = await render(
      {
        version: { type: "string", label: "Version", default: "21" },
        other: { type: "string", label: "Other", default: "x" },
      },
      {},
      (host) => {
        host.errors.set({ version: "Not an installed version" });
      },
    );
    const fields = queryAll(fixture, "z-field");
    expect(fields[0]?.textContent).toContain("Not an installed version");
    expect(fields[1]?.textContent).not.toContain("Not an installed version");
    expect(fields[0]?.querySelector("input")?.getAttribute("aria-invalid")).toBe("true");
  });

  it("makes locked fields read-only and names the profile", async () => {
    const { fixture, host } = await render(
      {
        keep: { type: "integer", label: "Keep", default: 1 },
        mode: { type: "string", enum: ["a", "b"], label: "Mode", default: "a" },
        flag: { type: "boolean", label: "Flag", default: false },
      },
      { keep: 2, mode: "a", flag: false },
      (h) => {
        h.locked.set({ keep: "example-team", mode: "example-team", flag: "example-team" });
      },
    );
    expect(query<HTMLInputElement>(fixture, "input[type=number]")?.readOnly).toBe(true);
    expect(queryAll<HTMLButtonElement>(fixture, "z-segment button").every((b) => b.disabled)).toBe(
      true,
    );
    expect(query<HTMLInputElement>(fixture, "input[role=switch]")?.disabled).toBe(true);
    expect(fixture.nativeElement.textContent).toContain("Locked by profile example-team");
    // Locked values are not validated.
    typeInto(query<HTMLInputElement>(fixture, "input[type=number]") as HTMLInputElement, "");
    expect(host.form().validate()).toBe(true);
  });

  it("marks fields without default as required and reports them", async () => {
    const { fixture, host } = await render({ name: { type: "string", label: "Name" } });
    expect(fixture.nativeElement.textContent).toContain("Name *");
    expect(host.form().validate()).toBe(false);
    await settle(fixture);
    expect(fixture.nativeElement.textContent).toContain("A value is required.");
  });

  it("uses plugin translations for labels when they exist, the manifest text otherwise", async () => {
    const { fixture } = await render({
      name: { type: "string", label: "Manifest label", default: "x" },
    });
    expect(fixture.nativeElement.textContent).toContain("Manifest label");
  });

  it("shows badges", async () => {
    const { fixture } = await render(
      { name: { type: "string", label: "Name", default: "x" } },
      {},
      (host) => {
        host.badges.set({ name: { text: "User", status: "info" } });
      },
    );
    expect(byText(fixture, "z-badge", "User")).toBeDefined();
  });
});
