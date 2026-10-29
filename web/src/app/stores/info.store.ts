import { Injectable, computed, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { Info } from "../api/types";
import { shortenHome } from "../core/paths";

/** Version, host and configured appearance of the core. */
@Injectable({ providedIn: "root" })
export class InfoStore {
  private readonly api = inject(ApiClient);

  readonly info = signal<Info | null>(null);
  readonly error = signal<ApiProblem | null>(null);
  /** Home directory of the user the core runs as, when the core tells. */
  readonly home = computed(() => this.info()?.home);
  private pending: Promise<Info | null> | null = null;

  /** Loads /info once; later calls return the same result. */
  load(): Promise<Info | null> {
    this.pending ??= this.fetch();
    return this.pending;
  }

  /** Loads /info again (the core may have been replaced by a new version). */
  reload(): Promise<Info | null> {
    this.pending = null;
    return this.load();
  }

  /** The text with the home directory written as `~`. */
  shorten(text: string): string {
    return shortenHome(text, this.home());
  }

  private async fetch(): Promise<Info | null> {
    try {
      const info = await this.api.info();
      this.info.set(info);
      this.error.set(null);
      return info;
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.pending = null;
      return null;
    }
  }
}
