import { NgComponentOutlet } from "@angular/common";
import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  effect,
  inject,
  signal,
  type Type,
} from "@angular/core";
import { ZBadge, ZButton, ZIcon } from "zenit-ui";
import { sessionBadge } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { DockStore } from "../stores/dock.store";
import { SessionsStore } from "../stores/sessions.store";
import { dockTitle } from "./dock-title";

/** The terminal dock at the bottom of every page: one tab per opened session. */
@Component({
  selector: "dbx-dock",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgComponentOutlet, ZBadge, ZButton, ZIcon],
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
            @if (sessionView(); as view) {
              <ng-container
                *ngComponentOutlet="
                  view;
                  inputs: {
                    sessionId: tab.id,
                    active: tab.id === dock.active() && !dock.collapsed(),
                  }
                "
              />
            }
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

  /**
   * The session view (terminal, steps, changes) is its own chunk, fetched when the first session
   * is opened; until then only the tab bar is part of the initial bundle.
   */
  protected readonly sessionView = signal<Type<unknown> | null>(null);

  constructor() {
    effect(() => {
      if (this.dock.tabs().length > 0 && this.sessionView() === null) {
        void import("./dock-session").then((m) => {
          this.sessionView.set(m.DockSession);
        });
      }
    });
  }

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
