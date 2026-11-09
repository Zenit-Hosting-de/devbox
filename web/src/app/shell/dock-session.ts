import { ChangeDetectionStrategy, Component, computed, inject, input } from "@angular/core";
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
import { dockTitle } from "./dock-title";

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
