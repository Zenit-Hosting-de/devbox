import {
  HttpErrorResponse,
  HttpHeaders,
  HttpResponse,
  type HttpEvent,
  type HttpInterceptorFn,
  type HttpRequest,
} from "@angular/common/http";
import { inject } from "@angular/core";
import { Observable, delay } from "rxjs";
import { environment } from "../../environments/environment";
import type { SessionCreate, SessionsQuery } from "../api/types";
import { MockBackend, MockProblem } from "./mock-backend";

type Answer = { status: number; body: unknown; headers?: Record<string, string> };

function ok(body: unknown): Answer {
  return { status: 200, body };
}

function route(req: HttpRequest<unknown>, backend: MockBackend, path: string): Answer {
  const method = req.method;
  const segments = path.split("/").filter(Boolean).map(decodeURIComponent);
  const [area, id, third, fourth] = segments;

  if (method === "GET" && area === "info" && !id) {
    return ok(backend.info());
  }
  if (area === "plugins") {
    if (method === "GET" && !id) {
      return ok(backend.pluginList(req.params.get("manifest") === "true"));
    }
    if (method === "GET" && id && third === "messages" && fourth) {
      return ok(backend.messages(id, fourth));
    }
  }
  if (area === "checks") {
    if (method === "GET" && !id) {
      return ok(backend.statusReport());
    }
    if (method === "GET" && id && id !== "refresh") {
      return ok(backend.check(id));
    }
    if (method === "POST" && id === "refresh") {
      const body = req.body as { ids?: string[] } | null;
      backend.refreshChecks(body?.ids);
      return { status: 202, body: null };
    }
  }
  if (area === "settings") {
    if (method === "GET" && !id) {
      return ok(backend.settingList());
    }
    if (method === "PATCH" && !id) {
      backend.patchSettings((req.body ?? {}) as Record<string, unknown>);
      return { status: 204, body: null };
    }
  }
  if (area === "credentials" && method === "GET" && !id) {
    return ok(backend.credentials());
  }
  if (area === "sessions") {
    if (method === "GET" && !id) {
      const query: SessionsQuery = {};
      const state = req.params.get("state");
      const plugin = req.params.get("plugin");
      const limit = req.params.get("limit");
      const cursor = req.params.get("cursor");
      if (state) query.state = state as SessionsQuery["state"];
      if (plugin) query.plugin = plugin;
      if (limit) query.limit = Number(limit);
      if (cursor) query.cursor = cursor;
      return ok(backend.listSessions(query));
    }
    if (method === "POST" && !id) {
      return { status: 201, body: backend.createSession(req.body as SessionCreate) };
    }
    if (method === "GET" && id && !third) {
      return ok(backend.session(id));
    }
    if (method === "GET" && id && third === "events") {
      return ok(backend.sessionEvents(id));
    }
    if (method === "GET" && id && third === "log") {
      const log = backend.sessionLog(id);
      return {
        status: 200,
        body: log.data.buffer.slice(log.data.byteOffset, log.data.byteOffset + log.data.byteLength),
        headers: { "X-DevBox-Next-Seq": String(log.nextSeq) },
      };
    }
    if (method === "POST" && id && third === "cancel") {
      return { status: 202, body: backend.cancelSession(id) };
    }
  }
  throw new MockProblem(404, "not-found", `No mock for ${method} ${path}.`);
}

/** Serves /api/v1/* from the mock backend. Everything else passes through. */
export const mockInterceptor: HttpInterceptorFn = (req, next) => {
  const prefix = environment.apiBase;
  const url = new URL(req.urlWithParams, "http://localhost");
  if (!url.pathname.startsWith(`${prefix}/`)) {
    return next(req);
  }
  const backend = inject(MockBackend);
  const path = url.pathname.slice(prefix.length);
  const answer = new Observable<HttpEvent<unknown>>((subscriber) => {
    try {
      const result = route(req, backend, path);
      subscriber.next(
        new HttpResponse({
          status: result.status,
          body: result.body,
          headers: new HttpHeaders(result.headers ?? {}),
          url: req.url,
        }),
      );
      subscriber.complete();
    } catch (error) {
      if (error instanceof MockProblem) {
        subscriber.error(
          new HttpErrorResponse({
            status: error.status,
            url: req.url,
            error: {
              type: `about:blank#${error.code}`,
              title: error.code,
              status: error.status,
              code: error.code,
              detail: error.detail,
              instance: path,
              ...(error.errors.length > 0 ? { errors: error.errors } : {}),
            },
          }),
        );
      } else {
        subscriber.error(error);
      }
    }
  });
  return backend.latency > 0 ? answer.pipe(delay(backend.latency)) : answer;
};
