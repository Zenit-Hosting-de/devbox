import type { HttpInterceptorFn } from "@angular/common/http";
import { catchError, throwError } from "rxjs";
import { toApiProblem } from "./problem";

const MUTATING = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** Header every modifying request carries (CSRF protection of the core). */
export const REQUEST_HEADER = "X-DevBox-Request";

/**
 * Adds the CSRF header to POST, PUT, PATCH and DELETE only, and turns failures into
 * ApiProblem instances (application/problem+json is parsed, transport errors are mapped).
 */
export const apiInterceptor: HttpInterceptorFn = (req, next) => {
  const prepared = MUTATING.has(req.method.toUpperCase())
    ? req.clone({ setHeaders: { [REQUEST_HEADER]: "1" } })
    : req;
  return next(prepared).pipe(catchError((error: unknown) => throwError(() => toApiProblem(error))));
};
