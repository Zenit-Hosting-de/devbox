import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  inject,
  input,
} from "@angular/core";
import { ZBadge, ZButton, ZIcon, ZToast } from "zenit-ui";
import { ApiProblem } from "../api/problem";
import { isLive } from "../api/types";
import { problemText, sessionBadge } from "../core/status";
import { WrapText } from "../core/wrap-text";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { DockStore } from "../stores/dock.store";
import { SessionsStore } from "../stores/sessions.store";
import type { TerminalSize } from "../terminal/terminal";
import { TerminalView } from "../terminal/terminal-view";
import { summarizeEvents } from "../ws/events";

/** One opened session: terminal, cancel, progress, steps and the result. */
@Component({
  selector: "dbx-dock-session",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [TerminalView, WrapText, ZBadge, ZButton, ZIcon],
  template: `
    <div class="body">
      <dbx-terminal
        [channel]="channel()"
        [interactive]="live()"
        [active]="active()"
        [label]="i18n.t('dock.terminalLabel', { title: title() })"
        (typed)="sessions.sendInput(sessionId(), $event)"
        (resized)="onResized($event)"
      />
      <div class="status">
        <div class="line">
          @if (live()) {
            <button zBtn="secondary" size="sm" type="button" (click)="cancel()">
              <z-icon name="stop" />{{ i18n.t("dock.cancel") }}
            </button>
          }
          @if (summary().progress; as progress) {
            @if (live()) {
              @if (progress.percent !== null) {
                <progress
                  class="bar"
                  max="100"
                  [value]="progress.percent"
                  [attr.aria-label]="i18n.t('dock.progress')"
                ></progress>
                <span class="z-mono">{{ progress.percent }} %</span>
              }
              @if (progress.text) {
                <span class="z-muted">{{ progress.text }}</span>
              }
            }
          }
          @if (!live() && session(); as s) {
            <z-badge [status]="sessionBadge(s.state)" dot>{{
              i18n.t("session.state." + s.state)
            }}</z-badge>
            @if (s.summary || summary().result) {
              <span>{{ s.summary ?? summary().result?.summary }}</span>
            }
          }
        </div>
        @if (!hasOutput() && started()) {
          <p class="z-muted note">{{ i18n.t("dock.noOutput") }}</p>
        }
        @if (summary().steps.length > 0) {
          <details class="steps" open>
            <summary>{{ i18n.t("dock.steps", { count: summary().steps.length }) }}</summary>
            <ol>
              @for (step of summary().steps; track step.id) {
                <li>
                  <span class="z-muted">{{ i18n.t("step.state." + step.state) }}:</span>
                  {{ step.label }}
                </li>
              }
            </ol>
          </details>
        }
        @if (summary().changes.length > 0) {
          <section>
            <h3 class="title-sm">
              {{
                i18n.t(session()?.dryRun ? "dock.changesPlanned" : "dock.changes", {
                  count: summary().changes.length,
                })
              }}
            </h3>
            <ul class="changes">
              @for (change of summary().changes; track $index) {
                <li class="change">
                  <span class="z-muted mark" aria-hidden="true">{{ opMark[change.op] }}</span>
                  <span class="z-visually-hidden">{{ i18n.t("change.op." + change.op) }}</span>
                  <span class="what">
                    <dbx-wrap-text class="z-mono" [text]="change.target" />
                    <span class="z-muted"><dbx-wrap-text [text]="change.summary" /></span>
                  </span>
                </li>
              }
            </ul>
          </section>
        }
        @if (summary().logs.length > 0) {
          <ul class="changes" [attr.aria-label]="i18n.t('dock.messages')">
            @for (entry of summary().logs; track $index) {
              <li>
                <z-badge [status]="logBadge[entry.level]">{{
                  i18n.t("log.level." + entry.level)
                }}</z-badge>
                <dbx-wrap-text [text]="entry.text" />
              </li>
            }
          </ul>
        }
        @for (notice of summary().notices; track $index) {
          <p class="z-muted">
            <z-icon name="info" size="sm" /> {{ i18n.t("notice." + notice.kind) }}
          </p>
        }
      </div>
    </div>
  `,
  styles: `
    /* Narrow: the terminal above, steps and changes below. Wide: side by side, so the list
       of changes has room even while the terminal stays empty. */
    .body {
      display: grid;
      grid-template-rows: minmax(0, 1fr) minmax(0, auto);
      gap: var(--space-2);
      block-size: 100%;
      min-block-size: 0;
    }
    @media (min-width: 48rem) {
      .body {
        grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
        grid-template-rows: minmax(0, 1fr);
        gap: var(--space-3);
      }
    }
    .status {
      display: grid;
      align-content: start;
      gap: var(--space-2);
      min-block-size: 0;
      overflow-y: auto;
    }
    .note {
      margin: 0;
    }
    .changes {
      margin: 0;
      padding: 0;
      list-style: none;
      display: grid;
      gap: var(--space-1);
    }
    .changes li {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: var(--space-2);
      min-inline-size: 0;
    }
    .change {
      flex-wrap: nowrap;
    }
    .mark {
      flex: none;
      inline-size: var(--space-3);
    }
    .what {
      display: flex;
      flex-direction: column;
      min-inline-size: 0;
    }
    .line {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-3);
      min-block-size: var(--control-sm);
    }
    .bar {
      inline-size: calc(var(--space-9) * 2);
      accent-color: var(--accent);
    }
    .steps summary {
      cursor: pointer;
    }
    .steps ol {
      margin: var(--space-2) 0 0;
      padding-inline-start: var(--space-5);
    }
  `,
})
export class DockSession {
  readonly sessionId = input.required<string>();
  readonly active = input(false);

  protected readonly i18n = inject(I18n);
  protected readonly sessions = inject(SessionsStore);
  private readonly dock = inject(DockStore);
  private readonly toast = inject(ZToast);
  private readonly text = inject(PluginText);

  protected readonly sessionBadge = sessionBadge;
  protected readonly opMark = { create: "+", update: "~", delete: "-" } as const;
  protected readonly logBadge = { info: "info", warn: "warning", error: "danger" } as const;

  protected readonly channel = computed(() => this.sessions.channel(this.sessionId()) ?? null);
  protected readonly session = computed(() => this.sessions.sessions().get(this.sessionId()));
  protected readonly live = computed(() => {
    const s = this.session();
    return s !== undefined && isLive(s.state);
  });
  protected readonly summary = computed(() => summarizeEvents(this.channel()?.events() ?? []));
  /** False while a plugin has printed nothing to the terminal, which the bundled ones do not. */
  protected readonly hasOutput = computed(() => (this.channel()?.outputBytes() ?? 0) > 0);
  /** The terminal has no pending log load and the session is known. */
  protected readonly started = computed(() => {
    const channel = this.channel();
    return this.session() !== undefined && channel !== null && !channel.loading();
  });
  protected readonly title = computed(() => dockTitle(this.text, this.session()));

  protected onResized(size: TerminalSize): void {
    if (this.active()) {
      this.dock.size.set(size);
    }
    if (this.live()) {
      this.sessions.sendResize(this.sessionId(), size.cols, size.rows);
    }
  }

  protected async cancel(): Promise<void> {
    try {
      await this.sessions.cancel(this.sessionId());
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.toast.error(problemText(this.i18n, error));
      } else {
        throw error;
      }
    }
  }
}

function dockTitle(
  text: PluginText,
  session: { action: string; title?: string | undefined } | undefined,
): string {
  if (!session) {
    return "";
  }
  const [plugin, action] = session.action.split(".");
  return plugin && action ? text.actionLabel(plugin, action, session.title) : session.action;
}

/** The terminal dock at the bottom of every page: one tab per opened session. */
@Component({
  selector: "dbx-dock",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DockSession, ZBadge, ZButton, ZIcon],
  template: `
    <section class="dock" [attr.aria-label]="i18n.t('dock.title')">
      <div class="bar">
        <div class="tabs">
          @if (dock.tabs().length > 0) {
            <div
              class="tablist"
              role="tablist"
              [attr.aria-label]="i18n.t('dock.tabs')"
              (keydown)="onKeydown($event)"
            >
              @for (tab of dock.tabs(); track tab.id) {
                @let session = sessions.sessions().get(tab.id);
                <button
                  type="button"
                  role="tab"
                  class="tab-label"
                  [class.active]="tab.id === dock.active()"
                  [id]="'dock-tab-' + tab.id"
                  [attr.aria-selected]="tab.id === dock.active()"
                  [attr.aria-controls]="'dock-panel-' + tab.id"
                  [attr.tabindex]="tab.id === dock.active() ? 0 : -1"
                  (click)="dock.select(tab.id)"
                >
                  @if (session) {
                    <z-badge [status]="badge(session.state)" dot>
                      {{ i18n.t("session.state." + session.state) }}
                    </z-badge>
                  }
                  <span>{{ titleOf(tab.id) }}</span>
                </button>
              }
            </div>
            @if (dock.active(); as activeId) {
              <button
                zBtn="ghost"
                size="sm"
                iconOnly
                type="button"
                [attr.aria-label]="i18n.t('dock.close', { title: titleOf(activeId) })"
                (click)="dock.close(activeId)"
              >
                <z-icon name="close" />
              </button>
            }
          } @else {
            <span class="z-subtle empty">{{ i18n.t("dock.empty") }}</span>
          }
        </div>
        <button
          zBtn="ghost"
          size="sm"
          iconOnly
          type="button"
          [attr.aria-expanded]="!dock.collapsed()"
          aria-controls="dock-panels"
          [attr.aria-label]="dock.collapsed() ? i18n.t('dock.expand') : i18n.t('dock.collapse')"
          (click)="dock.toggle()"
        >
          <z-icon [name]="dock.collapsed() ? 'expand_less' : 'expand_more'" />
        </button>
      </div>
      <div id="dock-panels" class="panels" [hidden]="dock.collapsed() || dock.tabs().length === 0">
        @for (tab of dock.tabs(); track tab.id) {
          <div
            role="tabpanel"
            class="panel"
            [id]="'dock-panel-' + tab.id"
            [attr.aria-labelledby]="'dock-tab-' + tab.id"
            [hidden]="tab.id !== dock.active()"
          >
            <dbx-dock-session
              [sessionId]="tab.id"
              [active]="tab.id === dock.active() && !dock.collapsed()"
            />
          </div>
        }
      </div>
    </section>
  `,
  styles: `
    :host {
      display: block;
      position: sticky;
      inset-block-end: 0;
      z-index: var(--z-header);
    }
    .dock {
      background: var(--surface);
      border-block-start: thin solid var(--border);
    }
    .bar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: var(--space-2);
      padding-inline: var(--space-3);
    }
    .tabs {
      display: flex;
      align-items: center;
      gap: var(--space-1);
      overflow-x: auto;
      min-inline-size: 0;
    }
    .tablist {
      display: flex;
      align-items: center;
      gap: var(--space-1);
      min-inline-size: 0;
      overflow-x: auto;
    }
    .tab-label.active {
      background: var(--surface-hover);
    }
    .tab-label {
      display: flex;
      align-items: center;
      gap: var(--space-2);
      min-block-size: var(--control-md);
      padding-inline: var(--space-2);
      background: none;
      border: 0;
      color: var(--text);
      font: inherit;
      cursor: pointer;
      white-space: nowrap;
      border-radius: var(--radius-sm);
    }
    .empty {
      padding-inline: var(--space-2);
    }
    .panels {
      padding: var(--space-2) var(--space-3) var(--space-3);
    }
    .panel {
      block-size: calc(var(--space-9) * 3);
    }
    [hidden] {
      display: none;
    }
  `,
})
export class Dock {
  protected readonly i18n = inject(I18n);
  protected readonly dock = inject(DockStore);
  protected readonly sessions = inject(SessionsStore);
  private readonly text = inject(PluginText);

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  protected badge = sessionBadge;

  /** Arrow keys, Home and End move between the tabs (roving tabindex); Delete closes one. */
  protected onKeydown(event: KeyboardEvent): void {
    const tabs = this.dock.tabs();
    const current = tabs.findIndex((t) => `dock-tab-${t.id}` === (event.target as Element).id);
    if (current < 0) {
      return;
    }
    let next: number;
    switch (event.key) {
      case "ArrowRight":
        next = (current + 1) % tabs.length;
        break;
      case "ArrowLeft":
        next = (current - 1 + tabs.length) % tabs.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = tabs.length - 1;
        break;
      case "Delete": {
        event.preventDefault();
        const id = tabs[current]?.id;
        const neighbour = tabs[current + 1] ?? tabs[current - 1];
        if (id !== undefined) {
          this.dock.close(id);
          if (neighbour) {
            this.focusTab(neighbour.id);
          }
        }
        return;
      }
      default:
        return;
    }
    event.preventDefault();
    const id = tabs[next]?.id;
    if (id !== undefined) {
      this.dock.select(id);
      this.focusTab(id);
    }
  }

  private focusTab(id: string): void {
    this.host.nativeElement.querySelector<HTMLElement>(`[id="dock-tab-${id}"]`)?.focus();
  }

  protected titleOf(id: string): string {
    return dockTitle(this.text, this.sessions.sessions().get(id)) || id;
  }
}
