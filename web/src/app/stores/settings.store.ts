import { Injectable, computed, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { SettingItem } from "../api/types";
import type { LoadState } from "./plugins.store";
import { SessionsStore } from "./sessions.store";

/** Effective settings with origin and lock (GET /settings). */
@Injectable({ providedIn: "root" })
export class SettingsStore {
  private readonly api = inject(ApiClient);

  readonly items = signal<readonly SettingItem[]>([]);
  /**
   * The settings per plugin. The arrays only change when the settings are reloaded, so a form
   * built from one keeps its edits while a search filter comes and goes.
   */
  readonly byPlugin = computed(() => {
    const map = new Map<string, SettingItem[]>();
    for (const item of this.items()) {
      const list = map.get(item.plugin) ?? [];
      list.push(item);
      map.set(item.plugin, list);
    }
    return map;
  });
  readonly state = signal<LoadState>("idle");
  readonly error = signal<ApiProblem | null>(null);
  private pending: Promise<void> | null = null;
  private again = false;

  constructor() {
    const sessions = inject(SessionsStore);
    sessions.on("settings", () => {
      void this.reload();
    });
    sessions.onReconnect(() => this.reload());
  }

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
   * Sets user values; null removes the user value. The core validates all keys first and
   * writes nothing when one fails. Rejects with an ApiProblem.
   */
  async patch(values: Readonly<Record<string, unknown>>): Promise<void> {
    try {
      await this.api.patchSettings(values);
    } catch (error) {
      throw toApiProblem(error);
    }
    await this.reload();
  }

  private async fetch(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    try {
      const list = await this.api.settings();
      this.items.set(list.settings);
      this.state.set("ready");
      this.error.set(null);
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.state.set(this.items().length > 0 ? "ready" : "error");
    }
  }
}
