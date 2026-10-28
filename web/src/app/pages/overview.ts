import { ChangeDetectionStrategy, Component, computed, inject } from "@angular/core";
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
  ZToast,
} from "zenit-ui";
import { ActionLauncher } from "../actions/action-launcher";
import { ApiProblem } from "../api/problem";
import type { CheckSummary, PluginDetail } from "../api/types";
import { checkBadge, pluginBadge, problemText } from "../core/status";
import { WrapText } from "../core/wrap-text";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { ChecksStore } from "../stores/checks.store";
import { InfoStore } from "../stores/info.store";
import { PluginsStore } from "../stores/plugins.store";

/** Order of the category groups; unknown categories follow alphabetically. */
export const CATEGORY_ORDER = [
  "core",
  "system",
  "runtime",
  "tool",
  "credentials",
  "consumer",
  "projects",
  "remote",
  "maintenance",
  "language",
  "other",
];

interface ActionButton {
  ref: string;
  label: string;
}

interface Hint {
  text: string;
  action?: ActionButton;
}

interface Tile {
  plugin: PluginDetail;
  name: string;
  description: string | undefined;
  ready: boolean;
  check: CheckSummary | undefined;
  checkLabel: string;
  running: boolean;
  hints: Hint[];
  primary: ActionButton | undefined;
}

interface Group {
  category: string;
  label: string;
  tiles: Tile[];
}

@Component({
  selector: "dbx-overview-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterLink,
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZIcon,
    ZPageHeader,
    ZPanel,
    ZSkeleton,
    WrapText,
  ],
  template: `
    <z-page-header [title]="i18n.t('nav.overview')" [sub]="infoLine()">
      <button
        zBtn="secondary"
        type="button"
        [loading]="checks.anyRunning()"
        [disabled]="plugins.state() !== 'ready'"
        (click)="refresh()"
      >
        <z-icon name="refresh" />{{ i18n.t("overview.refresh") }}
      </button>
    </z-page-header>

    @if (loadError(); as problem) {
      <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
        {{ problem }}
        <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="retry()">
          {{ i18n.t("common.retry") }}
        </button>
      </z-alert>
    } @else if (loading()) {
      <div
        class="tiles"
        role="status"
        [attr.aria-label]="i18n.t('common.loading')"
        aria-busy="true"
      >
        @for (n of placeholders; track n) {
          <z-panel>
            <div class="z-stack">
              <z-skeleton width="40%" />
              <z-skeleton width="80%" />
              <z-skeleton width="60%" />
            </div>
          </z-panel>
        }
      </div>
    } @else if (groups().length === 0) {
      <z-panel>
        <z-empty-state [title]="i18n.t('overview.empty')">
          {{ i18n.t("overview.emptyHint") }}
        </z-empty-state>
      </z-panel>
    } @else {
      <div class="groups">
        @for (group of groups(); track group.category) {
          <section class="group" [attr.aria-labelledby]="'cat-' + group.category">
            <h2 class="heading-2 group-title" [id]="'cat-' + group.category">{{ group.label }}</h2>
            <div class="tiles">
              @for (tile of group.tiles; track tile.plugin.id) {
                <z-panel [title]="tile.name" headingLevel="3" [busy]="tile.running">
                  <div class="z-stack tile">
                    @if (tile.description) {
                      <p class="z-muted">{{ tile.description }}</p>
                    }
                    @if (!tile.ready) {
                      <div class="z-cluster">
                        <z-badge [status]="pluginBadge(tile.plugin.state)" dot>
                          {{ i18n.t("plugin.state." + tile.plugin.state) }}
                        </z-badge>
                      </div>
                      <ul class="plain">
                        @for (reason of tile.plugin.reasons; track $index) {
                          <li>
                            {{ i18n.t("reason." + reason.code) }}
                            @if (reason.detail) {
                              <dbx-wrap-text class="z-mono" [text]="reason.detail" />
                            }
                          </li>
                        }
                      </ul>
                    } @else if (tile.check; as check) {
                      <div class="z-cluster">
                        <span class="z-muted">{{ tile.checkLabel }}</span>
                        <z-badge [status]="checkBadge(check.status)" dot>
                          {{ i18n.t("check.status." + check.status) }}
                        </z-badge>
                      </div>
                      <p class="summary"><dbx-wrap-text [text]="check.summary" /></p>
                      @for (hint of tile.hints; track $index) {
                        <div class="hint">
                          @if (hint.action; as action) {
                            <button
                              zBtn="secondary"
                              size="sm"
                              type="button"
                              (click)="start(action.ref)"
                            >
                              {{ action.label }}
                            </button>
                          } @else {
                            <span class="z-muted"><dbx-wrap-text [text]="hint.text" /></span>
                          }
                        </div>
                      }
                    } @else {
                      <p class="z-subtle">{{ i18n.t("overview.noCheck") }}</p>
                    }
                    <div class="z-cluster">
                      @if (tile.ready && tile.primary; as primary) {
                        <button zBtn="primary" size="sm" type="button" (click)="start(primary.ref)">
                          {{ primary.label }}
                        </button>
                      }
                      <a zBtn="ghost" size="sm" [routerLink]="['/plugins', tile.plugin.id]">
                        {{ i18n.t("overview.details") }}
                        <span class="z-visually-hidden">{{ tile.name }}</span>
                      </a>
                    </div>
                  </div>
                </z-panel>
              }
            </div>
          </section>
        }
      </div>
    }
  `,
  styles: `
    /* Most categories hold one or two plugins, so a row per category would leave the page
       mostly empty. The groups flow into columns instead (one on a phone, two or three on a
       desktop) and a group never breaks across two of them. */
    .groups {
      padding-block-start: var(--space-4);
      column-width: calc(var(--space-9) * 3.5);
      column-gap: var(--space-5);
    }
    .group {
      break-inside: avoid;
      margin-block-end: var(--space-5);
    }
    .tiles {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(min(100%, calc(var(--space-9) * 3)), 1fr));
      gap: var(--space-4);
    }
    .tiles > * {
      min-inline-size: 0;
    }
    .group-title {
      margin-block: 0 var(--space-3);
    }
    .summary {
      margin: 0;
      min-inline-size: 0;
    }
    .tile {
      gap: var(--space-3);
    }
    .hint {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-2);
    }
    .plain {
      margin: 0;
      padding-inline-start: var(--space-4);
    }
  `,
})
export class OverviewPage {
  protected readonly i18n = inject(I18n);
  protected readonly plugins = inject(PluginsStore);
  protected readonly checks = inject(ChecksStore);
  private readonly info = inject(InfoStore);
  private readonly text = inject(PluginText);
  private readonly launcher = inject(ActionLauncher);
  private readonly toast = inject(ZToast);

  protected readonly checkBadge = checkBadge;
  protected readonly pluginBadge = pluginBadge;
  protected readonly placeholders = [1, 2, 3];

  protected readonly loading = computed(() => {
    const states = [this.plugins.state(), this.checks.state()];
    return states.some((s) => s === "idle" || s === "loading");
  });

  protected readonly loadError = computed(() => {
    const problem = this.plugins.error() ?? this.checks.error();
    const failed = this.plugins.state() === "error" || this.checks.state() === "error";
    return failed && problem ? problemText(this.i18n, problem) : "";
  });

  protected readonly infoLine = computed(() => {
    const info = this.info.info();
    if (!info) {
      return "";
    }
    return this.i18n.t("overview.infoLine", {
      host: info.host.name,
      distro: `${info.host.distro} ${info.host.distroVersion}`.trim(),
      version: info.version,
    });
  });

  protected readonly groups = computed<Group[]>(() => {
    const byCategory = new Map<string, Tile[]>();
    for (const plugin of this.plugins.plugins()) {
      const category = plugin.category ?? plugin.manifest?.category ?? "other";
      const list = byCategory.get(category) ?? [];
      list.push(this.tile(plugin));
      byCategory.set(category, list);
    }
    const order = (c: string): number => {
      const index = CATEGORY_ORDER.indexOf(c);
      return index < 0 ? CATEGORY_ORDER.length : index;
    };
    return [...byCategory.entries()]
      .sort(([a], [b]) => order(a) - order(b) || a.localeCompare(b))
      .map(([category, tiles]) => ({
        category,
        label: this.text.category(category),
        tiles: tiles.sort((a, b) => a.name.localeCompare(b.name, this.i18n.locale())),
      }));
  });

  constructor() {
    void this.load();
  }

  protected retry(): void {
    void this.load(true);
  }

  protected async refresh(): Promise<void> {
    try {
      await this.checks.refresh();
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.toast.error(problemText(this.i18n, error));
      } else {
        throw error;
      }
    }
  }

  protected start(ref: string): void {
    void this.launcher.start(ref, { quick: true });
  }

  private async load(force = false): Promise<void> {
    await Promise.all([
      this.info.load(),
      force ? this.plugins.reload() : this.plugins.ensureLoaded(),
      force ? this.checks.reload() : this.checks.ensureLoaded(),
    ]);
  }

  private action(plugin: PluginDetail, ref: string): ActionButton | undefined {
    const [owner, name] = ref.split(".");
    if (!owner || !name) {
      return undefined;
    }
    const target = owner === plugin.id ? plugin : this.plugins.plugin(owner);
    const action = target?.manifest?.actions?.[name];
    if (!target || !action) {
      return undefined;
    }
    return { ref, label: this.text.actionLabel(owner, name, action.label) };
  }

  private tile(plugin: PluginDetail): Tile {
    const overview = plugin.manifest?.contributes?.overview;
    const checkRef = overview?.check;
    const check = checkRef ? this.checks.result(checkRef) : undefined;
    const checkName = checkRef?.split(".")[1] ?? "";
    const primary = overview?.primaryAction
      ? this.action(plugin, overview.primaryAction)
      : undefined;
    return {
      plugin,
      name: this.text.pluginName(plugin),
      description: this.text.pluginDescription(plugin),
      ready: plugin.state === "ready",
      check,
      checkLabel: this.text.checkLabel(plugin.id, checkName, check?.label),
      running: checkRef !== undefined && this.checks.running().has(checkRef),
      hints: (check?.hints ?? []).map((hint) => {
        const action = hint.action ? this.action(plugin, hint.action) : undefined;
        return { text: hint.text, ...(action ? { action } : {}) };
      }),
      primary,
    };
  }
}
