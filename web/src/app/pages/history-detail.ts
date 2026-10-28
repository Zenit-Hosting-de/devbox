import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from "@angular/core";
import { RouterLink } from "@angular/router";
import {
  ZAlert,
  ZAlertAction,
  ZBadge,
  ZButton,
  ZEmptyState,
  ZIcon,
  ZPageHeader,
  ZPanel,
  ZSkeleton,
  ZSpecList,
  type ZSpecItem,
} from "zenit-ui";
import { ApiClient } from "../api/api-client";
import { toApiProblem } from "../api/problem";
import type { Session, SessionEvent } from "../api/types";
import { isLive } from "../api/types";
import { problemText, sessionBadge } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { DockStore } from "../stores/dock.store";
import { PluginsStore } from "../stores/plugins.store";
import { SessionsStore } from "../stores/sessions.store";
import { TerminalView } from "../terminal/terminal-view";
import { summarizeEvents } from "../ws/events";

@Component({
  selector: "dbx-history-detail-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterLink,
    TerminalView,
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZIcon,
    ZPageHeader,
    ZPanel,
    ZSkeleton,
    ZSpecList,
  ],
  template: `
    @if (error()) {
      <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
        {{ error() }}
        <a zAlertAction zBtn="secondary" size="sm" routerLink="/verlauf">{{
          i18n.t("nav.history")
        }}</a>
      </z-alert>
    } @else if (session(); as s) {
      <z-page-header [title]="title()" [sub]="s.action">
        <a zBtn="ghost" routerLink="/verlauf"
          ><z-icon name="arrow_back" />{{ i18n.t("nav.history") }}</a
        >
      </z-page-header>
      <div class="z-stack">
        @if (live()) {
          <z-alert status="info" icon="terminal" [title]="i18n.t('history.runsInDock')">
            {{ i18n.t("history.runsInDockHint") }}
            <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="openInDock(s)">
              {{ i18n.t("history.openInDock") }}
            </button>
          </z-alert>
        }

        <z-panel [title]="i18n.t('history.details')" headingLevel="2">
          <div class="z-cluster state">
            <z-badge [status]="sessionBadge(s.state)" dot>{{
              i18n.t("session.state." + s.state)
            }}</z-badge>
            @if (s.dryRun) {
              <z-badge>{{ i18n.t("session.dryRun") }}</z-badge>
            }
          </div>
          <z-spec-list [items]="facts()" />
        </z-panel>

        <z-panel [title]="i18n.t('history.steps')" headingLevel="2">
          @if (summary().steps.length === 0) {
            <z-empty-state [title]="i18n.t('history.noSteps')" />
          } @else {
            <ol class="steps">
              @for (step of summary().steps; track step.id) {
                <li>
                  <z-badge [status]="stepBadge(step.state)" dot>
                    {{ i18n.t("step.state." + step.state) }}
                  </z-badge>
                  {{ step.label }}
                </li>
              }
            </ol>
          }
          @if (summary().changes.length > 0) {
            <h3 class="title-sm sub">{{ i18n.t("history.changes") }}</h3>
            <ul class="steps">
              @for (change of summary().changes; track $index) {
                <li>
                  <span class="z-mono">{{ change.target }}</span>
                  <span class="z-muted"> {{ change.summary }}</span>
                </li>
              }
            </ul>
          }
        </z-panel>

        <z-panel [title]="i18n.t('history.output')" headingLevel="2">
          @if (logLoading()) {
            <z-skeleton width="80%" />
          } @else {
            <div class="term">
              <dbx-terminal [data]="log()" [label]="i18n.t('history.outputLabel')" />
            </div>
          }
        </z-panel>
      </div>
    } @else {
      <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
        <z-skeleton width="30%" />
        <z-skeleton width="60%" />
      </div>
    }
  `,
  styles: `
    .state {
      margin-block-end: var(--space-3);
    }
    .steps {
      margin: 0;
      padding-inline-start: var(--space-4);
      display: grid;
      gap: var(--space-2);
    }
    .sub {
      margin-block: var(--space-4) var(--space-2);
    }
    .term {
      block-size: calc(var(--space-9) * 3);
    }
  `,
})
export class HistoryDetailPage {
  /** Route parameter. */
  readonly id = input.required<string>();

  protected readonly i18n = inject(I18n);
  private readonly api = inject(ApiClient);
  private readonly sessions = inject(SessionsStore);
  private readonly dock = inject(DockStore);
  private readonly text = inject(PluginText);
  private readonly plugins = inject(PluginsStore);

  protected readonly sessionBadge = sessionBadge;
  protected readonly error = signal("");
  private readonly loaded = signal<Session | null>(null);
  private readonly events = signal<readonly SessionEvent[]>([]);
  protected readonly log = signal<Uint8Array | null>(null);
  protected readonly logLoading = signal(true);
  private openedInDock = "";

  /** The summary, replaced by the live one when the WebSocket delivers updates. */
  protected readonly session = computed(() => {
    const loaded = this.loaded();
    return loaded ? (this.sessions.sessions().get(loaded.id) ?? loaded) : null;
  });
  protected readonly live = computed(() => {
    const s = this.session();
    return s !== null && isLive(s.state);
  });
  protected readonly summary = computed(() => summarizeEvents(this.events()));

  protected readonly title = computed(() => {
    const s = this.session();
    if (!s) {
      return "";
    }
    const [pluginId, action] = s.action.split(".");
    return pluginId && action
      ? this.text.actionLabel(pluginId, action, s.title)
      : (s.title ?? s.action);
  });

  protected readonly facts = computed<ZSpecItem[]>(() => {
    const s = this.session();
    if (!s) {
      return [];
    }
    const items: ZSpecItem[] = [
      { term: this.i18n.t("history.action"), value: s.action, mono: true },
    ];
    const add = (term: string, value: string | undefined, mono = false): void => {
      if (value) {
        items.push({ term, value, mono });
      }
    };
    add(this.i18n.t("history.plugin"), s.plugin);
    add(this.i18n.t("history.user"), s.user);
    add(
      this.i18n.t("history.origin"),
      s.origin ? this.i18n.t(`session.origin.${s.origin}`) : undefined,
    );
    add(this.i18n.t("history.started"), this.i18n.date(s.startedAt));
    add(this.i18n.t("history.ended"), this.i18n.date(s.endedAt));
    if (s.exitCode !== null && s.exitCode !== undefined) {
      add(this.i18n.t("history.exitCode"), String(s.exitCode), true);
    }
    add(this.i18n.t("history.changes"), String(s.changes ?? 0), true);
    add(this.i18n.t("history.summary"), s.summary);
    return items;
  });

  constructor() {
    void this.plugins.ensureLoaded();
    effect(() => {
      const id = this.id();
      untracked(() => {
        void this.load(id);
      });
    });
    // A session that still runs belongs in the dock.
    effect(() => {
      const s = this.session();
      if (s && isLive(s.state) && this.openedInDock !== s.id) {
        this.openedInDock = s.id;
        untracked(() => {
          void this.dock.openExisting(s);
        });
      }
    });
  }

  protected openInDock(session: Session): void {
    void this.dock.openExisting(session);
  }

  protected stepBadge(state: string): "success" | "info" | "danger" | "neutral" {
    switch (state) {
      case "done":
        return "success";
      case "running":
        return "info";
      case "failed":
        return "danger";
      default:
        return "neutral";
    }
  }

  private async load(id: string): Promise<void> {
    this.error.set("");
    this.logLoading.set(true);
    try {
      const session = await this.sessions.session(id);
      this.loaded.set(session);
      const [events, log] = await Promise.all([
        this.api.sessionEvents(id),
        this.api.sessionLog(id),
      ]);
      this.events.set(events.events);
      this.log.set(log.data);
    } catch (error) {
      this.error.set(problemText(this.i18n, toApiProblem(error)));
    } finally {
      this.logLoading.set(false);
    }
  }
}
