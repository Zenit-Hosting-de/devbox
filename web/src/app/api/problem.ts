import { HttpErrorResponse } from "@angular/common/http";
import type { ProblemCode, ProblemError } from "./types";

export type ApiProblemCode = ProblemCode | "network" | "unknown";

/** A failed API call: the parsed application/problem+json body, or a transport failure. */
export class ApiProblem extends Error {
  readonly code: ApiProblemCode;
  readonly status: number;
  readonly title: string;
  readonly detail: string | undefined;
  readonly errors: readonly ProblemError[];

  constructor(init: {
    code: ApiProblemCode;
    status: number;
    title?: string;
    detail?: string;
    errors?: readonly ProblemError[];
  }) {
    super(init.detail ?? init.title ?? init.code);
    this.name = "ApiProblem";
    this.code = init.code;
    this.status = init.status;
    this.title = init.title ?? init.code;
    this.detail = init.detail;
    this.errors = init.errors ?? [];
  }

  /** Field errors keyed by JSON pointer. */
  errorsByPointer(): Map<string, ProblemError> {
    const map = new Map<string, ProblemError>();
    for (const error of this.errors) {
      if (!map.has(error.pointer)) {
        map.set(error.pointer, error);
      }
    }
    return map;
  }
}

const problemCodes: ReadonlySet<string> = new Set<ProblemCode>([
  "validation",
  "not-found",
  "plugin-not-ready",
  "setting-locked",
  "permission-denied",
  "unauthenticated",
  "csrf",
  "conflict",
  "internal",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseErrors(value: unknown): ProblemError[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const errors: ProblemError[] = [];
  for (const entry of value) {
    if (
      isRecord(entry) &&
      typeof entry["pointer"] === "string" &&
      typeof entry["code"] === "string"
    ) {
      const message = entry["message"];
      errors.push({
        pointer: entry["pointer"],
        code: entry["code"],
        ...(typeof message === "string" ? { message } : {}),
      });
    }
  }
  return errors;
}

/** Turns a failed HttpClient call into an ApiProblem. */
export function toApiProblem(error: unknown): ApiProblem {
  if (error instanceof ApiProblem) {
    return error;
  }
  if (error instanceof HttpErrorResponse) {
    const body: unknown = error.error;
    if (isRecord(body) && typeof body["code"] === "string" && problemCodes.has(body["code"])) {
      return new ApiProblem({
        code: body["code"] as ProblemCode,
        status: typeof body["status"] === "number" ? body["status"] : error.status,
        ...(typeof body["title"] === "string" ? { title: body["title"] } : {}),
        ...(typeof body["detail"] === "string" ? { detail: body["detail"] } : {}),
        errors: parseErrors(body["errors"]),
      });
    }
    return new ApiProblem({
      code: error.status === 0 ? "network" : "unknown",
      status: error.status,
      title: error.statusText,
    });
  }
  return new ApiProblem({
    code: "unknown",
    status: 0,
    title: error instanceof Error ? error.message : "unknown",
  });
}
