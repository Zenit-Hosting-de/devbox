import { describe, expect, it } from "vitest";
import {
  fieldDefault,
  isRequired,
  parseOptionsFrom,
  resolveOptionsFrom,
  sameValue,
  validateField,
  type FormField,
} from "./field";

describe("optionsFrom pointer", () => {
  const results = {
    "java.installed": {
      status: "ok",
      data: { items: [{ version: "21.0.5" }, { version: "17.0.13" }, { version: "21.0.5" }] },
    },
    "node.installed": { data: { versions: ["22.12.0", 20, true, { nested: 1 }] } },
  };

  it("splits the check reference from the pointer", () => {
    expect(parseOptionsFrom("java.installed#/data/items/*/version")).toEqual({
      ref: "java.installed",
      segments: ["data", "items", "*", "version"],
    });
    expect(parseOptionsFrom("nopointer")).toBeNull();
  });

  it("expands * over every array item and keeps distinct values in order", () => {
    expect(resolveOptionsFrom("java.installed#/data/items/*/version", results)).toEqual([
      "21.0.5",
      "17.0.13",
    ]);
  });

  it("returns primitives only", () => {
    expect(resolveOptionsFrom("node.installed#/data/versions/*", results)).toEqual([
      "22.12.0",
      20,
      true,
    ]);
  });

  it("addresses array items by index", () => {
    expect(resolveOptionsFrom("java.installed#/data/items/1/version", results)).toEqual([
      "17.0.13",
    ]);
  });

  it("decodes ~1 and ~0 in pointer segments", () => {
    const special = { "a.b": { data: { "x/y": { "m~n": "found" } } } };
    expect(resolveOptionsFrom("a.b#/data/x~1y/m~0n", special)).toEqual(["found"]);
  });

  it("is null while the check has no result and empty for a missing path", () => {
    expect(resolveOptionsFrom("java.latest#/data/items/*/version", results)).toBeNull();
    expect(resolveOptionsFrom("java.installed#/data/nothing/*", results)).toEqual([]);
  });
});

describe("defaults and required", () => {
  it("treats unresolved placeholders as no default", () => {
    expect(fieldDefault({ type: "string", default: "${xdg:data}/jdk" })).toBeUndefined();
    expect(fieldDefault({ type: "integer", default: 21 })).toBe(21);
  });

  it("requires a value only without a default; secrets and booleans never", () => {
    expect(isRequired({ type: "string" })).toBe(true);
    expect(isRequired({ type: "string", default: "x" })).toBe(false);
    expect(isRequired({ type: "string", secret: true })).toBe(false);
    expect(isRequired({ type: "boolean" })).toBe(false);
  });
});

describe("validateField", () => {
  const cases: {
    name: string;
    field: FormField;
    value: unknown;
    required: boolean;
    code?: string;
  }[] = [
    {
      name: "empty required",
      field: { type: "string" },
      value: "",
      required: true,
      code: "required",
    },
    { name: "empty optional", field: { type: "string" }, value: undefined, required: false },
    {
      name: "pattern fails",
      field: { type: "string", pattern: "^[a-z]+$" },
      value: "A1",
      required: false,
      code: "pattern",
    },
    {
      name: "pattern ok",
      field: { type: "string", pattern: "^[a-z]+$" },
      value: "abc",
      required: false,
    },
    {
      name: "uri fails",
      field: { type: "string", format: "uri" },
      value: "not a url",
      required: false,
      code: "uri",
    },
    {
      name: "uri ok",
      field: { type: "string", format: "uri" },
      value: "https://example.com/x",
      required: false,
    },
    {
      name: "below minimum",
      field: { type: "integer", minimum: 1 },
      value: 0,
      required: false,
      code: "minimum",
    },
    {
      name: "above maximum",
      field: { type: "integer", maximum: 5 },
      value: 6,
      required: false,
      code: "maximum",
    },
    {
      name: "not an integer",
      field: { type: "integer" },
      value: 1.5,
      required: false,
      code: "integer",
    },
    {
      name: "enum mismatch",
      field: { type: "integer", enum: [8, 11] },
      value: 9,
      required: false,
      code: "enum",
    },
    { name: "enum match", field: { type: "integer", enum: [8, 11] }, value: 11, required: false },
    {
      name: "too few items",
      field: { type: "array", items: { type: "string" }, minItems: 2 },
      value: ["a"],
      required: false,
      code: "minItems",
    },
    {
      name: "duplicate items",
      field: { type: "array", items: { type: "string" }, uniqueItems: true },
      value: ["a", "a"],
      required: false,
      code: "uniqueItems",
    },
    {
      name: "item pattern",
      field: { type: "array", items: { type: "string", pattern: "^[a-z]+$" } },
      value: ["ok", "NO"],
      required: false,
      code: "pattern",
    },
  ];

  it.each(cases)("$name", ({ field, value, required, code }) => {
    const error = validateField(field, value, required);
    expect(error?.code).toBe(code);
  });

  it("names the offending array item", () => {
    const error = validateField(
      { type: "array", items: { type: "string", pattern: "^[a-z]+$" } },
      ["ok", "NO"],
      false,
    );
    expect(error?.index).toBe(1);
  });
});

describe("sameValue", () => {
  it("compares scalars and arrays by value", () => {
    expect(sameValue(["a", "b"], ["a", "b"])).toBe(true);
    expect(sameValue(["a"], ["a", "b"])).toBe(false);
    expect(sameValue(1, 1)).toBe(true);
    expect(sameValue(undefined, undefined)).toBe(true);
  });
});
