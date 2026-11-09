import { Injectable, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { Profile, ProfileCreate, ProfileUpdate } from "../api/types";
import type { LoadState } from "./plugins.store";
import { SessionsStore } from "./sessions.store";

/** Profile sources of the system and the user configuration (GET /profiles). */
@Injectable({ providedIn: "root" })
export class ProfilesStore {
  private readonly api = inject(ApiClient);

  readonly profiles = signal<readonly Profile[]>([]);
  readonly state = signal<LoadState>("idle");
  readonly error = signal<ApiProblem | null>(null);
  /** True while the form adds a profile (the fetch runs inside the call). */
  readonly saving = signal(false);
  /** True while all remote profiles are fetched again. */
  readonly updating = signal(false);
  /** IDs with a removal in flight. */
  readonly busy = signal<ReadonlySet<string>>(new Set());

  constructor() {
    const sessions = inject(SessionsStore);
    // a profile switches plugins and sets settings; both announce a change
    sessions.on("settings", () => (this.state() === "idle" ? undefined : void this.reload()));
    sessions.onReconnect(() => (this.state() === "idle" ? undefined : this.reload()));
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
      this.profiles.set(await this.api.profiles());
      this.state.set("ready");
      this.error.set(null);
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.state.set(this.profiles().length > 0 ? "ready" : "error");
    }
  }

  /** Fetches and adds a profile; rejects with an ApiProblem when nothing was written. */
  async add(body: ProfileCreate): Promise<Profile> {
    this.saving.set(true);
    try {
      const created = await this.api.createProfile(body);
      this.profiles.update((list) => [...list, created]);
      return created;
    } catch (error) {
      throw toApiProblem(error);
    } finally {
      this.saving.set(false);
    }
  }

  /** Fetches all remote profiles again and shows the new state. */
  async update(): Promise<readonly ProfileUpdate[]> {
    this.updating.set(true);
    try {
      const updates = await this.api.updateProfiles();
      await this.reload();
      return updates;
    } catch (error) {
      throw toApiProblem(error);
    } finally {
      this.updating.set(false);
    }
  }

  async remove(id: string): Promise<void> {
    this.setBusy(id, true);
    try {
      await this.api.deleteProfile(id);
      this.profiles.update((list) => list.filter((p) => p.id !== id));
    } catch (error) {
      throw toApiProblem(error);
    } finally {
      this.setBusy(id, false);
    }
  }

  private setBusy(id: string, on: boolean): void {
    this.busy.update((set) => {
      const next = new Set(set);
      if (on) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  }
}
