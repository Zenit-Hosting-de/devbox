import { Injectable, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { Info } from "../api/types";

/** Version, host and configured appearance of the core. */
@Injectable({ providedIn: "root" })
export class InfoStore {
  private readonly api = inject(ApiClient);

  readonly info = signal<Info | null>(null);
  readonly error = signal<ApiProblem | null>(null);
  private pending: Promise<Info | null> | null = null;

  /** Loads /info once; later calls return the same result. */
  load(): Promise<Info | null> {
    this.pending ??= this.fetch();
    return this.pending;
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
