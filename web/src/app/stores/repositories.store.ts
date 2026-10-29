import { Injectable, computed, inject, signal } from "@angular/core";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { Repository, RepositoryCreate } from "../api/types";
import { PluginsStore, type LoadState } from "./plugins.store";
import { SessionsStore } from "./sessions.store";

/** Plugin repositories with their sync state (GET /repositories) and the actions on them. */
@Injectable({ providedIn: "root" })
export class RepositoriesStore {
  private readonly api = inject(ApiClient);
  private readonly plugins = inject(PluginsStore);

  readonly repositories = signal<readonly Repository[]>([]);
  readonly state = signal<LoadState>("idle");
  readonly error = signal<ApiProblem | null>(null);
  /** True while a new source is being added (the first sync runs inside the call). */
  readonly adding = signal(false);
  /** Names with a call in flight (sync, approve, discard, remove). */
  readonly busy = signal<ReadonlySet<string>>(new Set());
  /** Sources that wait for approval. */
  readonly pendingCount = computed(
    () => this.repositories().filter((r) => r.pending !== undefined).length,
  );
  private pending: Promise<void> | null = null;
  private again = false;

  constructor() {
    const sessions = inject(SessionsStore);
    sessions.on("plugins", () => {
      if (this.state() !== "idle") {
        void this.reload();
      }
    });
    sessions.onReconnect(() => (this.state() === "idle" ? undefined : this.reload()));
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

  repository(name: string): Repository | undefined {
    return this.repositories().find((r) => r.name === name);
  }

  /**
   * Adds a source and syncs it once. Resolves with the entry, which can have the state
   * error when the sync failed; rejects with an ApiProblem when the entry was not written.
   */
  async add(body: RepositoryCreate): Promise<Repository> {
    this.adding.set(true);
    try {
      const created = await this.api.createRepository(body);
      this.upsert(created);
      return created;
    } catch (error) {
      throw toApiProblem(error);
    } finally {
      this.adding.set(false);
    }
  }

  sync(name: string): Promise<Repository> {
    return this.call(name, () => this.api.syncRepository(name));
  }

  /** `revision` is the pending revision the user reviewed; the core refuses another one. */
  async approve(name: string, revision: string): Promise<Repository> {
    const result = await this.call(name, () => this.api.approveRepository(name, revision));
    void this.plugins.reload();
    return result;
  }

  discard(name: string): Promise<Repository> {
    return this.call(name, () => this.api.discardRepository(name));
  }

  async remove(name: string): Promise<void> {
    this.setBusy(name, true);
    try {
      await this.api.deleteRepository(name);
      this.repositories.update((list) => list.filter((r) => r.name !== name));
    } catch (error) {
      throw toApiProblem(error);
    } finally {
      this.setBusy(name, false);
    }
    void this.plugins.reload();
  }

  private async call(name: string, run: () => Promise<Repository>): Promise<Repository> {
    this.setBusy(name, true);
    try {
      const result = await run();
      this.upsert(result);
      return result;
    } catch (error) {
      throw toApiProblem(error);
    } finally {
      this.setBusy(name, false);
    }
  }

  private upsert(repository: Repository): void {
    this.repositories.update((list) => {
      const rest = list.filter((r) => r.name !== repository.name);
      return [...rest, repository].sort((a, b) => a.name.localeCompare(b.name));
    });
  }

  private setBusy(name: string, on: boolean): void {
    this.busy.update((set) => {
      const next = new Set(set);
      if (on) {
        next.add(name);
      } else {
        next.delete(name);
      }
      return next;
    });
  }

  private async fetch(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    try {
      this.repositories.set(await this.api.repositories());
      this.state.set("ready");
      this.error.set(null);
    } catch (error) {
      this.error.set(toApiProblem(error));
      this.state.set(this.repositories().length > 0 ? "ready" : "error");
    }
  }
}
