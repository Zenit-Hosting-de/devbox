import { ChangeDetectionStrategy, Component, computed, inject, signal } from "@angular/core";
import { RouterLink } from "@angular/router";
import {
  ZAlert,
  ZAlertAction,
  ZBadge,
  ZButton,
  ZEmptyState,
  ZField,
  ZIcon,
  ZPageHeader,
  ZPanel,
  ZRow,
  ZRowAction,
  ZRowLink,
  ZRowMain,
  ZRowTitle,
  ZRows,
  ZRowsHead,
  ZSelect,
  ZSkeleton,
} from "zenit-ui";
import { ApiClient } from "../api/api-client";
import { toApiProblem } from "../api/problem";
import type { Session, SessionState } from "../api/types";
import { isLive } from "../api/types";
import { problemText, sessionBadge } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { DockStore } from "../stores/dock.store";
import { PluginsStore } from "../stores/plugins.store";
import { SessionsStore } from "../stores/sessions.store";

export const HISTORY_PAGE_SIZE = 25;

export const SESSION_STATES: readonly SessionState[] = [
  "queued",
  "running",
  "success",
  "failed",
  "blocked",
  "stopped",
  "timeout",
];

@Component({
  selector: "dbx-history-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterLink,
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZField,
    ZIcon,
    ZPageHeader,
    ZPanel,
    ZRow,
    ZRowAction,
    ZRowLink,
    ZRowMain,
    ZRowTitle,
    ZRows,
    ZRowsHead,
    ZSelect,
    ZSkeleton,
  ],
  template: `
    <z-page-header [title]="i18n.t('nav.history')" [sub]="i18n.t('history.sub')" />

    <div class="filters">
      <z-field [label]="i18n.t('history.filterState')" for="hist-state">
        <z-select>
          <select id="hist-state" (change)="onState($event)">
            <option value="" [selected]="state() === ''">{{ i18n.t("history.all") }}</option>
            @for (s of states; track s) {
              <option [value]="s" [selected]="state() === s">
                {{ i18n.t("session.state." + s) }}
              </option>
            }
          </select>
        </z-select>
      </z-field>
      <z-field [label]="i18n.t('history.filterPlugin')" for="hist-plugin">
        <z-select>
          <select id="hist-plugin" (change)="onPlugin($event)">
            <option value="" [selected]="plugin() === ''">{{ i18n.t("history.all") }}</option>
            @for (p of pluginOptions(); track p.id) {
              <option [value]="p.id" [selected]="plugin() === p.id">{{ p.name }}</option>
            }
          </select>
        </z-select>
      </z-field>
    </div>

    @if (error()) {
      <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
        {{ error() }}
        <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="reload()">
          {{ i18n.t("common.retry") }}
        </button>
      </z-alert>
    } @else {
      <z-panel flush [busy]="loading()">
        @if (loading() && rows().length === 0) {
          <div class="z-stack pad" role="status" [attr.aria-label]="i18n.t('common.loading')">
            <z-skeleton width="70%" />
            <z-skeleton width="50%" />
            <z-skeleton width="60%" />
          </div>
        } @else if (rows().length === 0) {
          <z-empty-state [title]="i18n.t('history.empty')">{{
            i18n.t("history.emptyHint")
          }}</z-empty-state>
        } @else {
          <z-rows
            columns="minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1fr) minmax(0, 2fr) auto"
          >
            <z-rows-head>
              <span>{{ i18n.t("history.action") }}</span>
              <span>{{ i18n.t("history.state") }}</span>
              <span>{{ i18n.t("history.time") }}</span>
              <span>{{ i18n.t("history.changes") }}</span>
              <span>{{ i18n.t("history.summary") }}</span>
              <span></span>
            </z-rows-head>
            @for (row of rows(); track row.session.id) {
              <div zRow>
                <z-row-main [title]="row.title" [meta]="row.meta">
                  <a zRowTitle zRowLink [routerLink]="['/verlauf', row.session.id]">{{
                    row.title
                  }}</a>
                </z-row-main>
                <span class="z-cluster">
                  <z-badge [status]="sessionBadge(row.session.state)" dot>
                    {{ i18n.t("session.state." + row.session.state) }}
                  </z-badge>
                  @if (row.session.dryRun) {
                    <z-badge>{{ i18n.t("session.dryRun") }}</z-badge>
                  }
                </span>
                <span class="z-muted">
                  {{ i18n.date(row.session.startedAt, "short") }}
                  @if (row.session.endedAt) {
                    <br />{{ i18n.date(row.session.endedAt, "short") }}
                  }
                </span>
                <span class="z-mono">{{ row.session.changes ?? 0 }}</span>
                <span class="z-muted">{{ row.session.summary }}</span>
                @if (row.live) {
                  <button
                    zRowAction
                    zBtn="secondary"
                    size="sm"
                    type="button"
                    (click)="openInDock(row.session)"
                  >
                    <z-icon name="terminal" />{{ i18n.t("history.openInDock") }}
                  </button>
                } @else {
                  <span></span>
                }
              </div>
            }
          </z-rows>
          @if (next()) {
            <div class="more">
              <button zBtn="secondary" type="button" [loading]="loading()" (click)="loadMore()">
                {{ i18n.t("history.more") }}
              </button>
            </div>
          }
        }
      </z-panel>
    }
  `,
  styles: `
    .filters {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(calc(var(--space-9) * 2), 1fr));
      gap: var(--space-4);
      margin-block-end: var(--space-5);
    }
    .pad {
      padding: var(--space-4);
    }
    .more {
      display: flex;
      justify-content: center;
      padding: var(--space-4);
      border-block-start: thin solid var(--border);
    }
  `,
})
export class HistoryPage {
  protected readonly i18n = inject(I18n);
  private readonly api = inject(ApiClient);
  private readonly text = inject(PluginText);
  private readonly plugins = inject(PluginsStore);
  private readonly sessions = inject(SessionsStore);
  private readonly dock = inject(DockStore);

  protected readonly states = SESSION_STATES;
  protected readonly sessionBadge = sessionBadge;

  protected readonly state = signal<SessionState | "">("");
  protected readonly plugin = signal("");
  private readonly listed = signal<readonly Session[]>([]);
  protected readonly next = signal<string | undefined>(undefined);
  protected readonly loading = signal(false);
  protected readonly error = signal("");

  protected readonly pluginOptions = computed(() =>
    this.plugins
      .plugins()
      .map((p) => ({ id: p.id, name: this.text.pluginName(p) }))
      .sort((a, b) => a.name.localeCompare(b.name, this.i18n.locale())),
  );

  /** The listed sessions, each replaced by the live summary when the WebSocket knows a newer one. */
  protected readonly rows = computed(() => {
    const live = this.sessions.sessions();
    return this.listed().map((listed) => {
      const session = live.get(listed.id) ?? listed;
      return {
        session,
        live: isLive(session.state),
        title: this.titleOf(session),
        meta: session.action,
      };
    });
  });

  constructor() {
    void this.plugins.ensureLoaded();
    void this.load(false);
  }

  protected onState(event: Event): void {
    this.state.set((event.target as HTMLSelectElement).value as SessionState | "");
    void this.load(false);
  }

  protected onPlugin(event: Event): void {
    this.plugin.set((event.target as HTMLSelectElement).value);
    void this.load(false);
  }

  protected reload(): void {
    void this.load(false);
  }

  protected loadMore(): void {
    void this.load(true);
  }

  protected openInDock(session: Session): void {
    void this.dock.openExisting(session);
  }

  private titleOf(session: Session): string {
    const [pluginId, action] = session.action.split(".");
    if (!pluginId || !action) {
      return session.title ?? session.action;
    }
    return this.text.actionLabel(pluginId, action, session.title);
  }

  private async load(append: boolean): Promise<void> {
    this.loading.set(true);
    this.error.set("");
    try {
      const state = this.state();
      const plugin = this.plugin();
      const cursor = this.next();
      const page = await this.api.sessions({
        limit: HISTORY_PAGE_SIZE,
        ...(state ? { state } : {}),
        ...(plugin ? { plugin } : {}),
        ...(append && cursor ? { cursor } : {}),
      });
      this.listed.update((list) => (append ? [...list, ...page.sessions] : page.sessions));
      this.next.set(page.next);
    } catch (error) {
      this.error.set(problemText(this.i18n, toApiProblem(error)));
    } finally {
      this.loading.set(false);
    }
  }
}
