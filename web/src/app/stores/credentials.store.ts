import { Injectable, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { CredentialInstance } from "../api/types";
import type { LoadState } from "./plugins.store";
import { SessionsStore } from "./sessions.store";

/** Credential instances without their secrets (read-only in this milestone). */
@Injectable({ providedIn: "root" })
export class CredentialsStore {
  private readonly api = inject(ApiClient);

  readonly instances = signal<readonly CredentialInstance[]>([]);
  readonly state = signal<LoadState>("idle");
  readonly error = signal<ApiProblem | null>(null);

  constructor() {
    inject(SessionsStore).onReconnect(() => this.reload());
  }

  ensureLoaded(): Promise<void> {
    if (this.state() === "ready") {
      return Promise.resolve();
    }
    return this.reload();
  }

  async reload(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    try {
      this.instances.set((await this.api.credentials()).instances);
      this.state.set("ready");
      this.error.set(null);
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.state.set(this.instances().length > 0 ? "ready" : "error");
    }
  }
}
