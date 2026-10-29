import { Injectable, computed, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { PluginDetail } from "../api/types";
import { SessionsStore } from "./sessions.store";

export type LoadState = "idle" | "loading" | "ready" | "error";

/** All plugins with their manifests (GET /plugins?manifest=true). */
@Injectable({ providedIn: "root" })
export class PluginsStore {
  private readonly api = inject(ApiClient);

  readonly plugins = signal<readonly PluginDetail[]>([]);
  readonly state = signal<LoadState>("idle");
  readonly error = signal<ApiProblem | null>(null);
  readonly byId = computed(() => new Map(this.plugins().map((p) => [p.id, p])));
  private pending: Promise<void> | null = null;
  private again = false;

  constructor() {
    const sessions = inject(SessionsStore);
    sessions.on("plugins", () => {
      void this.reload();
    });
    sessions.onReconnect(() => this.reload());
  }

  /** Loads the plugins unless they are loaded or loading. */
  ensureLoaded(): Promise<void> {
    if (this.state() === "ready") {
      return Promise.resolve();
    }
    return this.reload();
  }

  /** Reloads the list; a reload asked for while one runs triggers exactly one more. */
  reload(): Promise<void> {
    if (this.pending) {
      this.again = true;
      return this.pending;
    }
    this.pending = this.fetch().finally(() => {
      this.pending = null;
      if (this.again) {
        this.again = false;
        void this.reload();
      }
    });
    return this.pending;
  }

  /**
   * Switches a plugin on or off for the user. Rejects with an ApiProblem (409 for core
   * plugins and for plugins that the system configuration or a profile switches off).
   */
  async setEnabled(id: string, enabled: boolean): Promise<void> {
    try {
      await this.api.patchPlugin(id, enabled);
    } catch (error) {
      throw toApiProblem(error);
    }
    await this.reload();
  }

  plugin(id: string): PluginDetail | undefined {
    return this.byId().get(id);
  }

  private async fetch(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    try {
      this.plugins.set(await this.api.plugins(true));
      this.state.set("ready");
      this.error.set(null);
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.state.set(this.plugins().length > 0 ? "ready" : "error");
    }
  }
}
