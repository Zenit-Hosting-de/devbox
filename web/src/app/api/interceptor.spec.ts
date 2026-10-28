import { HttpClient, provideHttpClient, withInterceptors } from "@angular/common/http";
import { HttpTestingController, provideHttpClientTesting } from "@angular/common/http/testing";
import { provideZonelessChangeDetection } from "@angular/core";
import { TestBed } from "@angular/core/testing";
import { firstValueFrom } from "rxjs";
import { beforeEach, describe, expect, it } from "vitest";
import { ApiClient } from "./api-client";
import { apiInterceptor, REQUEST_HEADER } from "./interceptor";
import { ApiProblem } from "./problem";

describe("apiInterceptor", () => {
  let http: HttpClient;
  let controller: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        provideHttpClient(withInterceptors([apiInterceptor])),
        provideHttpClientTesting(),
      ],
    });
    http = TestBed.inject(HttpClient);
    controller = TestBed.inject(HttpTestingController);
  });

  it.each(["POST", "PUT", "PATCH", "DELETE"] as const)("adds the header to %s", (method) => {
    http.request(method, "/api/v1/x", { body: {} }).subscribe();
    const req = controller.expectOne("/api/v1/x");
    expect(req.request.headers.get(REQUEST_HEADER)).toBe("1");
    req.flush({});
  });

  it.each(["GET", "HEAD", "OPTIONS"] as const)("does not add the header to %s", (method) => {
    http.request(method, "/api/v1/x").subscribe();
    const req = controller.expectOne("/api/v1/x");
    expect(req.request.headers.has(REQUEST_HEADER)).toBe(false);
    req.flush({});
  });

  it("parses application/problem+json into a typed ApiProblem with field errors", async () => {
    const result = firstValueFrom(http.patch("/api/v1/settings", { "java.keepPatches": 9 }));
    controller.expectOne("/api/v1/settings").flush(
      {
        type: "about:blank",
        title: "Validation failed",
        status: 422,
        code: "validation",
        detail: "One value is invalid.",
        errors: [
          { pointer: "/java.keepPatches", code: "maximum", message: "at most 5" },
          { pointer: "/params/version", code: "enum" },
          { nonsense: true },
        ],
      },
      {
        status: 422,
        statusText: "Unprocessable Entity",
        headers: { "Content-Type": "application/problem+json" },
      },
    );
    const problem = await result.catch((error: unknown) => error);
    expect(problem).toBeInstanceOf(ApiProblem);
    const typed = problem as ApiProblem;
    expect(typed.code).toBe("validation");
    expect(typed.status).toBe(422);
    expect(typed.detail).toBe("One value is invalid.");
    expect(typed.errors).toEqual([
      { pointer: "/java.keepPatches", code: "maximum", message: "at most 5" },
      { pointer: "/params/version", code: "enum" },
    ]);
    expect(typed.errorsByPointer().get("/java.keepPatches")?.message).toBe("at most 5");
  });

  it("maps a transport failure to the network code and an unknown body to unknown", async () => {
    const network = firstValueFrom(http.get("/api/v1/info"));
    controller.expectOne("/api/v1/info").error(new ProgressEvent("error"), { status: 0 });
    expect(((await network.catch((e: unknown) => e)) as ApiProblem).code).toBe("network");

    const odd = firstValueFrom(http.get("/api/v1/info"));
    controller.expectOne("/api/v1/info").flush("boom", { status: 502, statusText: "Bad Gateway" });
    const problem = (await odd.catch((e: unknown) => e)) as ApiProblem;
    expect(problem.code).toBe("unknown");
    expect(problem.status).toBe(502);
  });

  it("is used by the typed client: cancel is a POST with the header, listing a GET without", async () => {
    const api = TestBed.inject(ApiClient);
    const list = api.sessions({ state: "running", limit: 10 });
    const get = controller.expectOne((r) => r.url === "/api/v1/sessions");
    expect(get.request.params.get("state")).toBe("running");
    expect(get.request.params.get("limit")).toBe("10");
    expect(get.request.headers.has(REQUEST_HEADER)).toBe(false);
    get.flush({ sessions: [] });
    await list;

    const cancel = api.cancelSession("s_0123456789");
    const post = controller.expectOne("/api/v1/sessions/s_0123456789/cancel");
    expect(post.request.method).toBe("POST");
    expect(post.request.headers.get(REQUEST_HEADER)).toBe("1");
    post.flush({ id: "s_0123456789", action: "a.b", state: "running", dryRun: false });
    await cancel;
  });
});
