import { Injectable, inject, signal } from "@angular/core";
import type { Session } from "../api/types";
import { STORAGE_KEYS, readStored, writeStored } from "../core/storage";
import type { TerminalSize } from "../terminal/terminal";
import { SessionsStore } from "./sessions.store";

export interface DockTab {
  /** Session ID. */
  id: string;
}

/** Terminal size used for new sessions until the dock terminal reported its own. */
export const DEFAULT_TERMINAL_SIZE: TerminalSize = { cols: 120, rows: 32 };

/** The tabs of the terminal dock: one per opened session. */
@Injectable({ providedIn: "root" })
export class DockStore {
  private readonly sessions = inject(SessionsStore);

  readonly tabs = signal<readonly DockTab[]>([]);
  readonly active = signal<string | null>(null);
  readonly collapsed = signal(readStored(STORAGE_KEYS.dockCollapsed) === "1");
  /** Size of the dock terminal; sent as cols/rows when a session is created. */
  readonly size = signal<TerminalSize>(DEFAULT_TERMINAL_SIZE);

  /** Opens a session that was just created. */
  openNew(session: Session): void {
    this.sessions.openNew(session);
    this.show(session.id);
  }

  /** Opens a session that already runs: log first, then the stream. */
  async openExisting(session: Session): Promise<void> {
    const opening = this.sessions.openExisting(session);
    this.show(session.id);
    await opening;
  }

  select(id: string): void {
    if (this.tabs().some((t) => t.id === id)) {
      this.active.set(id);
      this.setCollapsed(false);
    }
  }

  close(id: string): void {
    const tabs = this.tabs();
    const index = tabs.findIndex((t) => t.id === id);
    if (index < 0) {
      return;
    }
    this.sessions.closeChannel(id);
    const rest = tabs.filter((t) => t.id !== id);
    this.tabs.set(rest);
    if (this.active() === id) {
      this.active.set(rest[Math.min(index, rest.length - 1)]?.id ?? null);
    }
  }

  toggle(): void {
    this.setCollapsed(!this.collapsed());
  }

  setCollapsed(value: boolean): void {
    this.collapsed.set(value);
    writeStored(STORAGE_KEYS.dockCollapsed, value ? "1" : "0");
  }

  private show(id: string): void {
    if (!this.tabs().some((t) => t.id === id)) {
      this.tabs.update((tabs) => [...tabs, { id }]);
    }
    this.active.set(id);
    this.setCollapsed(false);
  }
}
