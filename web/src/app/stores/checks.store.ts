import { Injectable, computed, effect, inject, signal, untracked } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { CheckSummary, Ref } from "../api/types";
import { I18n } from "../i18n/i18n";
import { SessionsStore } from "./sessions.store";
import type { LoadState } from "./plugins.store";

/** Cached check results. `check` messages of the WebSocket keep them current. */
@Injectable({ providedIn: "root" })
export class ChecksStore {
  private readonly api = inject(ApiClient);
  private readonly i18n = inject(I18n);

  readonly results = signal<Readonly<Record<string, CheckSummary>>>({});
  /** Checks that are running right now. */
  readonly running = signal<ReadonlySet<string>>(new Set());
  readonly state = signal<LoadState>("idle");
  readonly error = signal<ApiProblem | null>(null);
  readonly anyRunning = computed(() => this.running().size > 0);
  private pending: Promise<void> | null = null;
  /** The language the results were requested for. */
  private loadedLanguage = "";

  constructor() {
    const sessions = inject(SessionsStore);
    sessions.on("check", (message) => {
      this.apply(message.id, message.result);
    });
    sessions.onReconnect(() => this.reload());

    // The checks run with DEVBOX_LANG of the UI language: after a switch the core runs them
    // again, and the new results arrive as `check` messages.
    effect(() => {
      const language = this.i18n.language();
      untracked(() => {
        if (this.state() === "ready" && language !== this.loadedLanguage) {
          void this.reload();
        }
      });
    });
  }

  ensureLoaded(): Promise<void> {
    if (this.state() === "ready") {
      return Promise.resolve();
    }
    return this.reload();
  }

  reload(): Promise<void> {
    this.pending ??= this.fetch().finally(() => {
      this.pending = null;
    });
    return this.pending;
  }

  result(ref: string): CheckSummary | undefined {
    return this.results()[ref];
  }

  /** Runs checks again; without ids, all of them. The results arrive as `check` messages. */
  async refresh(ids?: readonly Ref[]): Promise<void> {
    const targets = ids && ids.length > 0 ? ids : Object.keys(this.results());
    this.running.update((set) => new Set([...set, ...targets]));
    try {
      await this.api.refreshChecks(ids, this.i18n.language());
    } catch (error) {
      this.running.update((set) => {
        const next = new Set(set);
        for (const id of targets) {
          next.delete(id);
        }
        return next;
      });
      throw toApiProblem(error);
    }
  }

  private apply(id: string, result: CheckSummary): void {
    this.results.update((all) => ({ ...all, [id]: result }));
    this.running.update((set) => {
      if (!set.has(id)) {
        return set;
      }
      const next = new Set(set);
      next.delete(id);
      return next;
    });
  }

  private async fetch(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    const language = this.i18n.language();
    try {
      const report = await this.api.checks(language);
      this.loadedLanguage = language;
      const results: Record<string, CheckSummary> = {};
      for (const check of report.checks) {
        results[check.id] = check;
      }
      this.results.set(results);
      this.running.set(new Set(report.running ?? []));
      this.state.set("ready");
      this.error.set(null);
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.state.set(Object.keys(this.results()).length > 0 ? "ready" : "error");
    }
  }
}
