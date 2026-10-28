import { HttpClient, HttpParams } from "@angular/common/http";
import { Injectable, inject } from "@angular/core";
import { firstValueFrom } from "rxjs";
import { environment } from "../../environments/environment";
import type {
  CheckSummary,
  CredentialList,
  Info,
  Messages,
  PluginList,
  Ref,
  Session,
  SessionCreate,
  SessionEventList,
  SessionList,
  SessionsQuery,
  SettingList,
  StatusReport,
} from "./types";

export interface SessionLog {
  /** Raw terminal output, secrets already masked by the core. */
  data: Uint8Array;
  /** Every output chunk with a lower seq is contained in data. */
  nextSeq: number;
}

/** Typed REST client for the core API. Failures reject with an ApiProblem. */
@Injectable({ providedIn: "root" })
export class ApiClient {
  private readonly http = inject(HttpClient);
  private readonly base = environment.apiBase;

  info(): Promise<Info> {
    return firstValueFrom(this.http.get<Info>(`${this.base}/info`));
  }

  plugins(manifest: boolean): Promise<PluginList> {
    const params = new HttpParams().set("manifest", manifest);
    return firstValueFrom(this.http.get<PluginList>(`${this.base}/plugins`, { params }));
  }

  pluginMessages(plugin: string, lang: string): Promise<Messages> {
    return firstValueFrom(
      this.http.get<Messages>(
        `${this.base}/plugins/${encodeURIComponent(plugin)}/messages/${encodeURIComponent(lang)}`,
      ),
    );
  }

  /** `lang`: results produced for another language run again (DEVBOX_LANG of the checks). */
  checks(lang?: string): Promise<StatusReport> {
    const params = lang ? new HttpParams().set("lang", lang) : undefined;
    return firstValueFrom(
      this.http.get<StatusReport>(`${this.base}/checks`, params ? { params } : {}),
    );
  }

  check(id: Ref): Promise<CheckSummary> {
    return firstValueFrom(
      this.http.get<CheckSummary>(`${this.base}/checks/${encodeURIComponent(id)}`),
    );
  }

  async refreshChecks(ids?: readonly Ref[], lang?: string): Promise<void> {
    const body = {
      ...(ids && ids.length > 0 ? { ids: [...ids] } : {}),
      ...(lang ? { lang } : {}),
    };
    await firstValueFrom(this.http.post(`${this.base}/checks/refresh`, body));
  }

  settings(): Promise<SettingList> {
    return firstValueFrom(this.http.get<SettingList>(`${this.base}/settings`));
  }

  async patchSettings(patch: Readonly<Record<string, unknown>>): Promise<void> {
    await firstValueFrom(this.http.patch(`${this.base}/settings`, patch));
  }

  credentials(): Promise<CredentialList> {
    return firstValueFrom(this.http.get<CredentialList>(`${this.base}/credentials`));
  }

  sessions(query: SessionsQuery = {}): Promise<SessionList> {
    let params = new HttpParams();
    if (query.state) {
      params = params.set("state", query.state);
    }
    if (query.plugin) {
      params = params.set("plugin", query.plugin);
    }
    if (query.limit !== undefined) {
      params = params.set("limit", query.limit);
    }
    if (query.cursor) {
      params = params.set("cursor", query.cursor);
    }
    return firstValueFrom(this.http.get<SessionList>(`${this.base}/sessions`, { params }));
  }

  session(id: string): Promise<Session> {
    return firstValueFrom(
      this.http.get<Session>(`${this.base}/sessions/${encodeURIComponent(id)}`),
    );
  }

  sessionEvents(id: string): Promise<SessionEventList> {
    return firstValueFrom(
      this.http.get<SessionEventList>(`${this.base}/sessions/${encodeURIComponent(id)}/events`),
    );
  }

  async sessionLog(id: string): Promise<SessionLog> {
    const response = await firstValueFrom(
      this.http.get(`${this.base}/sessions/${encodeURIComponent(id)}/log`, {
        observe: "response",
        responseType: "arraybuffer",
      }),
    );
    const next = Number(response.headers.get("X-DevBox-Next-Seq"));
    return {
      data: new Uint8Array(response.body ?? new ArrayBuffer(0)),
      nextSeq: Number.isFinite(next) && next >= 0 ? next : 0,
    };
  }

  createSession(body: SessionCreate): Promise<Session> {
    return firstValueFrom(this.http.post<Session>(`${this.base}/sessions`, body));
  }

  cancelSession(id: string): Promise<Session> {
    return firstValueFrom(
      this.http.post<Session>(`${this.base}/sessions/${encodeURIComponent(id)}/cancel`, null),
    );
  }
}
