import { ChangeDetectionStrategy, Component, computed, inject, input } from "@angular/core";
import { ZBadge, ZButton, ZIcon, ZToast } from "zenit-ui";
import { ApiProblem } from "../api/problem";
import { isLive } from "../api/types";
import { problemText, sessionBadge } from "../core/status";
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
  imports: [TerminalView, ZBadge, ZButton, ZIcon],
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
        @if (summary().steps.length > 0) {
          <details class="steps">
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
        @for (notice of summary().notices; track $index) {
          <p class="z-muted">
            <z-icon name="info" size="sm" /> {{ i18n.t("notice." + notice.kind) }}
          </p>
        }
      </div>
    </div>
  `,
  styles: `
    .body {
      display: grid;
      grid-template-rows: minmax(0, 1fr) auto;
      gap: var(--space-2);
      block-size: 100%;
      min-block-size: 0;
    }
    .status {
      display: grid;
      gap: var(--space-1);
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

  protected readonly channel = computed(() => this.sessions.channel(this.sessionId()) ?? null);
  protected readonly session = computed(() => this.sessions.sessions().get(this.sessionId()));
  protected readonly live = computed(() => {
    const s = this.session();
    return s !== undefined && isLive(s.state);
  });
  protected readonly summary = computed(() => summarizeEvents(this.channel()?.events() ?? []));
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
        <div class="tabs" role="tablist" [attr.aria-label]="i18n.t('dock.tabs')">
          @for (tab of dock.tabs(); track tab.id) {
            @let session = sessions.sessions().get(tab.id);
            <div class="tab" [class.active]="tab.id === dock.active()">
              <button
                type="button"
                role="tab"
                class="tab-label"
                [id]="'dock-tab-' + tab.id"
                [attr.aria-selected]="tab.id === dock.active()"
                [attr.aria-controls]="'dock-panel-' + tab.id"
                (click)="dock.select(tab.id)"
              >
                @if (session) {
                  <z-badge [status]="badge(session.state)" dot>
                    {{ i18n.t("session.state." + session.state) }}
                  </z-badge>
                }
                <span>{{ titleOf(tab.id) }}</span>
              </button>
              <button
                zBtn="ghost"
                size="sm"
                iconOnly
                type="button"
                [attr.aria-label]="i18n.t('dock.close', { title: titleOf(tab.id) })"
                (click)="dock.close(tab.id)"
              >
                <z-icon name="close" />
              </button>
            </div>
          } @empty {
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
    .tab {
      display: flex;
      align-items: center;
      border-radius: var(--radius-sm);
    }
    .tab.active {
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

  protected badge = sessionBadge;

  protected titleOf(id: string): string {
    return dockTitle(this.text, this.sessions.sessions().get(id)) || id;
  }
}
