import { DOCUMENT } from "@angular/common";
import { Injectable, InjectionToken, inject, signal } from "@angular/core";
import { InfoStore } from "./info.store";
import { SessionsStore } from "./sessions.store";

/** Loads the page again, so that the UI bundle of the new version is fetched. */
export const PAGE_RELOAD = new InjectionToken<() => void>("PAGE_RELOAD", {
  providedIn: "root",
  factory: () => {
    const doc = inject(DOCUMENT);
    return () => {
      doc.location.reload();
    };
  },
});

/**
 * The core restarts itself with a new binary (after DevBox updated itself). The WebSocket
 * message `restart` raises the banner; the reconnect of the SessionsStore (it keeps trying
 * with backoff while the new process starts) reloads /info. A changed version reloads the
 * page, an unchanged one (the restart did not happen) only hides the banner.
 */
@Injectable({ providedIn: "root" })
export class RestartStore {
  private readonly info = inject(InfoStore);
  private readonly reloadPage = inject(PAGE_RELOAD);

  /** True from the announcement until the core is back. */
  readonly restarting = signal(false);
  private versionBefore: string | undefined;

  constructor() {
    const sessions = inject(SessionsStore);
    sessions.on("restart", () => {
      this.begin();
    });
    sessions.onReconnect(() => this.finish());
  }

  private begin(): void {
    if (this.restarting()) {
      return;
    }
    this.versionBefore = this.info.info()?.version;
    this.restarting.set(true);
  }

  private async finish(): Promise<void> {
    if (!this.restarting()) {
      return;
    }
    const next = await this.info.reload();
    if (next !== null && this.versionBefore !== undefined && next.version !== this.versionBefore) {
      this.reloadPage(); // the banner stays until the page is gone
      return;
    }
    this.restarting.set(false);
  }
}
