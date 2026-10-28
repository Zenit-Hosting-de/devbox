// Field schemas of settings, action parameters and credential fields (plugin.v1.json
// $defs/field), plus the pure helpers SchemaForm builds on: options from check data and
// client-side validation.

export type FieldType = "string" | "integer" | "number" | "boolean" | "array" | "object";

export interface FormField {
  type: FieldType;
  label?: string;
  help?: string;
  default?: unknown;
  advanced?: boolean;
  secret?: boolean;
  optionsFrom?: string;
  enum?: unknown[];
  const?: unknown;
  format?: "uri" | "date-time" | "path";
  pattern?: string;
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: number;
  exclusiveMaximum?: number;
  minLength?: number;
  maxLength?: number;
  items?: FormField;
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  examples?: unknown[];
}

/** A value that is a typed form value: what SchemaForm emits. */
export type FormValue = string | number | boolean | string[] | undefined;
export type FormValues = Record<string, unknown>;

// ---------------------------------------------------------------------------
// optionsFrom: "<plugin>.<check>#/data/items/*/version"
// ---------------------------------------------------------------------------

export interface OptionsSource {
  /** Check reference, <plugin>.<check>. */
  ref: string;
  /** JSON pointer into the check result; "*" stands for every item of an array. */
  segments: string[];
}

export function parseOptionsFrom(spec: string): OptionsSource | null {
  const hash = spec.indexOf("#");
  if (hash <= 0) {
    return null;
  }
  const ref = spec.slice(0, hash);
  const pointer = spec.slice(hash + 1);
  const segments = pointer
    .split("/")
    .slice(1)
    .map((s) => s.replaceAll("~1", "/").replaceAll("~0", "~"));
  return { ref, segments };
}

function walk(node: unknown, segments: readonly string[], out: unknown[]): void {
  if (segments.length === 0) {
    out.push(node);
    return;
  }
  const [head, ...rest] = segments as [string, ...string[]];
  if (head === "*") {
    if (Array.isArray(node)) {
      for (const item of node) {
        walk(item, rest, out);
      }
    }
    return;
  }
  if (Array.isArray(node)) {
    const index = Number(head);
    if (Number.isInteger(index) && index >= 0 && index < node.length) {
      walk(node[index], rest, out);
    }
    return;
  }
  if (node !== null && typeof node === "object" && Object.hasOwn(node, head)) {
    walk((node as Record<string, unknown>)[head], rest, out);
  }
}

/**
 * Resolves an optionsFrom pointer against the cached check results. Returns the distinct
 * primitive values in order of appearance, or null while the check has no result.
 */
export function resolveOptionsFrom(
  spec: string,
  results: Readonly<Record<string, unknown>>,
): (string | number | boolean)[] | null {
  const source = parseOptionsFrom(spec);
  if (!source) {
    return null;
  }
  const result = results[source.ref];
  if (result === undefined) {
    return null;
  }
  const found: unknown[] = [];
  walk(result, source.segments, found);
  const seen = new Set<string>();
  const options: (string | number | boolean)[] = [];
  for (const value of found) {
    if (typeof value !== "string" && typeof value !== "number" && typeof value !== "boolean") {
      continue;
    }
    const key = `${typeof value}:${String(value)}`;
    if (!seen.has(key)) {
      seen.add(key);
      options.push(value);
    }
  }
  return options;
}

// ---------------------------------------------------------------------------
// Defaults and required
// ---------------------------------------------------------------------------

export function isPlaceholder(value: unknown): boolean {
  return typeof value === "string" && value.includes("${");
}

/** The default of a field as a form value; undefined for placeholders that are not resolved. */
export function fieldDefault(field: FormField): unknown {
  return isPlaceholder(field.default) ? undefined : field.default;
}

/** Required means: no default (and, checked by the caller, no value). Secrets are never required. */
export function isRequired(field: FormField): boolean {
  return field.default === undefined && field.secret !== true && field.type !== "boolean";
}

export function isEmpty(value: unknown): boolean {
  return (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0)
  );
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

export interface FieldError {
  /** Message key suffix: form.error.<code>. */
  code: string;
  params?: Record<string, string | number>;
  /** Index of the offending array item. */
  index?: number;
}

function matches(pattern: string, value: string): boolean {
  try {
    return new RegExp(pattern).test(value);
  } catch {
    return true;
  }
}

function isUri(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol.length > 1 && (url.host !== "" || url.protocol === "file:");
  } catch {
    return false;
  }
}

function validateScalar(field: FormField, value: unknown): FieldError | null {
  if (field.enum && !field.enum.some((e) => e === value)) {
    return { code: "enum" };
  }
  if (field.const !== undefined && field.const !== value) {
    return { code: "enum" };
  }
  if (field.type === "integer" || field.type === "number") {
    if (typeof value !== "number" || !Number.isFinite(value)) {
      return { code: "number" };
    }
    if (field.type === "integer" && !Number.isInteger(value)) {
      return { code: "integer" };
    }
    if (field.minimum !== undefined && value < field.minimum) {
      return { code: "minimum", params: { min: field.minimum } };
    }
    if (field.maximum !== undefined && value > field.maximum) {
      return { code: "maximum", params: { max: field.maximum } };
    }
    if (field.exclusiveMinimum !== undefined && value <= field.exclusiveMinimum) {
      return { code: "minimum", params: { min: field.exclusiveMinimum } };
    }
    if (field.exclusiveMaximum !== undefined && value >= field.exclusiveMaximum) {
      return { code: "maximum", params: { max: field.exclusiveMaximum } };
    }
    return null;
  }
  if (field.type === "string") {
    if (typeof value !== "string") {
      return { code: "string" };
    }
    if (field.minLength !== undefined && value.length < field.minLength) {
      return { code: "minLength", params: { min: field.minLength } };
    }
    if (field.maxLength !== undefined && value.length > field.maxLength) {
      return { code: "maxLength", params: { max: field.maxLength } };
    }
    if (field.pattern !== undefined && !matches(field.pattern, value)) {
      return { code: "pattern" };
    }
    if (field.format === "uri" && !isUri(value)) {
      return { code: "uri" };
    }
  }
  return null;
}

/**
 * Validates one value against its field. `required` is decided by the caller (usually
 * isRequired(field)). Returns null when the value is acceptable.
 */
export function validateField(
  field: FormField,
  value: unknown,
  required: boolean,
): FieldError | null {
  if (isEmpty(value)) {
    return required ? { code: "required" } : null;
  }
  if (field.type === "array") {
    if (!Array.isArray(value)) {
      return { code: "array" };
    }
    if (field.minItems !== undefined && value.length < field.minItems) {
      return { code: "minItems", params: { min: field.minItems } };
    }
    if (field.maxItems !== undefined && value.length > field.maxItems) {
      return { code: "maxItems", params: { max: field.maxItems } };
    }
    if (field.uniqueItems && new Set(value).size !== value.length) {
      return { code: "uniqueItems" };
    }
    const item = field.items ?? { type: "string" as const };
    for (const [index, entry] of value.entries()) {
      const error = validateField(item, entry, true);
      if (error) {
        return { ...error, index };
      }
    }
    return null;
  }
  if (field.type === "boolean") {
    return typeof value === "boolean" ? null : { code: "boolean" };
  }
  return validateScalar(field, value);
}

/** Structural equality for form values (scalars and arrays of scalars). */
export function sameValue(a: unknown, b: unknown): boolean {
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((v, i) => v === b[i]);
  }
  return a === b;
}
