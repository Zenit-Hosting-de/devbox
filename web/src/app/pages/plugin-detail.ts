import { ChangeDetectionStrategy, Component, computed, inject, input } from "@angular/core";
import { RouterLink } from "@angular/router";
import {
  ZAlert,
  ZBadge,
  ZButton,
  ZEmptyState,
  ZIcon,
  ZPageHeader,
  ZPanel,
  ZPanelActions,
  ZSkeleton,
  ZSpecList,
  ZToast,
  type ZSpecItem,
} from "zenit-ui";
import { ActionLauncher } from "../actions/action-launcher";
import { ApiProblem } from "../api/problem";
import type { CheckSummary } from "../api/types";
import { checkBadge, pluginBadge, problemText } from "../core/status";
import { WrapText } from "../core/wrap-text";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { SettingsGroup } from "../settings/settings-group";
import { ChecksStore } from "../stores/checks.store";
import { InfoStore } from "../stores/info.store";
import { PluginsStore } from "../stores/plugins.store";
import { SettingsStore } from "../stores/settings.store";

interface CheckView {
  ref: string;
  label: string;
  result: CheckSummary | undefined;
  details: ZSpecItem[];
  running: boolean;
}

interface ActionView {
  name: string;
  label: string;
  danger: boolean;
  sudo: boolean;
}

@Component({
  selector: "dbx-plugin-detail-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterLink,
    SettingsGroup,
    ZAlert,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZIcon,
    ZPageHeader,
    ZPanel,
    ZPanelActions,
    ZSkeleton,
    ZSpecList,
    WrapText,
  ],
  template: `
    @if (plugin(); as p) {
      <z-page-header [title]="name()" [sub]="subtitle()">
        <a zBtn="ghost" routerLink="/plugins"
          ><z-icon name="arrow_back" />{{ i18n.t("nav.plugins") }}</a
        >
      </z-page-header>

      <div class="z-stack">
        @if (description()) {
          <p class="z-muted">{{ description() }}</p>
        }

        @if (p.state !== "ready") {
          <z-alert
            status="warning"
            icon="block"
            [title]="i18n.t('plugin.notReady', { state: i18n.t('plugin.state.' + p.state) })"
          >
            @for (reason of p.reasons; track $index) {
              <div>
                {{ i18n.t("reason." + reason.code) }}
                @if (reason.detail) {
                  <dbx-wrap-text class="z-mono" [text]="reason.detail" />
                }
              </div>
            }
          </z-alert>
        }

        <z-panel [title]="i18n.t('plugin.checks')" headingLevel="2" [busy]="checks.anyRunning()">
          <button
            zPanelActions
            zBtn="secondary"
            size="sm"
            type="button"
            [loading]="anyCheckRunning()"
            [disabled]="checkViews().length === 0"
            (click)="refreshChecks()"
          >
            <z-icon name="refresh" />{{ i18n.t("plugin.refreshChecks") }}
          </button>
          @if (checks.state() === "idle" || checks.state() === "loading") {
            <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
              <z-skeleton width="50%" />
              <z-skeleton width="70%" />
            </div>
          } @else if (checkViews().length === 0) {
            <z-empty-state [title]="i18n.t('plugin.noChecks')" />
          } @else {
            <div class="z-stack">
              @for (check of checkViews(); track check.ref) {
                <article class="check" [attr.aria-label]="check.label">
                  <div class="z-cluster">
                    <h3 class="title-sm">{{ check.label }}</h3>
                    @if (check.result; as result) {
                      <z-badge [status]="checkBadge(result.status)" dot>
                        {{ i18n.t("check.status." + result.status) }}
                      </z-badge>
                      <dbx-wrap-text [text]="result.summary" />
                    } @else {
                      <span class="z-subtle">{{ i18n.t("plugin.noResult") }}</span>
                    }
                  </div>
                  @if (check.result; as result) {
                    @if (check.details.length > 0) {
                      <z-spec-list [items]="check.details" />
                    }
                    @for (hint of result.hints ?? []; track $index) {
                      <div class="hint">
                        @if (p.state === "ready" && hint.action; as action) {
                          <button
                            zBtn="secondary"
                            size="sm"
                            type="button"
                            (click)="startRef(action)"
                          >
                            {{ actionLabelOf(action) }}
                          </button>
                        } @else {
                          <span class="z-muted"><dbx-wrap-text [text]="hint.text" /></span>
                        }
                      </div>
                    }
                    <span class="z-subtle caption">
                      {{ i18n.t("plugin.checkedAt", { time: i18n.date(result.checkedAt) }) }}
                    </span>
                  }
                </article>
              }
            </div>
          }
        </z-panel>

        <z-panel [title]="i18n.t('plugin.actions')" headingLevel="2">
          @if (p.state !== "ready") {
            <p class="z-muted">{{ i18n.t("plugin.actionsUnavailable") }}</p>
          } @else if (actionViews().length === 0) {
            <z-empty-state [title]="i18n.t('plugin.noActions')" />
          } @else {
            <div class="z-cluster">
              @for (action of actionViews(); track action.name) {
                <button
                  [zBtn]="action.danger ? 'danger' : 'secondary'"
                  type="button"
                  (click)="start(action.name)"
                >
                  @if (action.sudo) {
                    <z-icon name="lock" size="sm" />
                  }
                  {{ action.label }}
                  @if (action.sudo) {
                    <span class="z-visually-hidden">{{ i18n.t("action.sudoShort") }}</span>
                  }
                </button>
              }
            </div>
            @if (hasSudo()) {
              <p class="z-subtle caption sudo-note">
                <z-icon name="lock" size="sm" /> {{ i18n.t("action.sudoHint") }}
              </p>
            }
          }
        </z-panel>

        <z-panel [title]="i18n.t('plugin.settings')" headingLevel="2">
          @if (settings.state() === "idle" || settings.state() === "loading") {
            <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
              <z-skeleton width="60%" />
              <z-skeleton width="40%" />
            </div>
          } @else if (pluginSettings().length === 0) {
            <z-empty-state [title]="i18n.t('plugin.noSettings')" />
          } @else {
            <dbx-settings-group [pluginId]="p.id" [items]="pluginSettings()" />
          }
        </z-panel>
      </div>
    } @else if (plugins.state() === "ready" || plugins.state() === "error") {
      <z-empty-state [title]="i18n.t('plugin.notFound')" headingLevel="1">
        <a zEmptyAction zBtn="secondary" routerLink="/plugins">{{ i18n.t("nav.plugins") }}</a>
      </z-empty-state>
    } @else {
      <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
        <z-skeleton width="30%" />
        <z-skeleton width="60%" />
      </div>
    }
  `,
  styles: `
    .check {
      display: grid;
      gap: var(--space-2);
      padding-block-end: var(--space-4);
      border-block-end: thin solid var(--border);
    }
    .check:last-child {
      border-block-end: 0;
      padding-block-end: 0;
    }
    .hint {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-2);
    }
    .sudo-note {
      margin-block-start: var(--space-3);
    }
  `,
})
export class PluginDetailPage {
  /** Route parameter. */
  readonly id = input.required<string>();

  protected readonly i18n = inject(I18n);
  protected readonly plugins = inject(PluginsStore);
  protected readonly checks = inject(ChecksStore);
  protected readonly settings = inject(SettingsStore);
  private readonly info = inject(InfoStore);
  private readonly text = inject(PluginText);
  private readonly launcher = inject(ActionLauncher);
  private readonly toast = inject(ZToast);

  protected readonly checkBadge = checkBadge;
  protected readonly pluginBadge = pluginBadge;

  protected readonly plugin = computed(() => this.plugins.byId().get(this.id()));
  protected readonly manifest = computed(() => this.plugin()?.manifest);

  protected readonly name = computed(() => {
    const plugin = this.plugin();
    return plugin ? this.text.pluginName(plugin) : this.id();
  });
  protected readonly description = computed(() => {
    const plugin = this.plugin();
    return plugin ? this.text.pluginDescription(plugin) : undefined;
  });
  protected readonly subtitle = computed(() => {
    const plugin = this.plugin();
    return plugin
      ? this.i18n.t("plugin.subtitle", {
          version: plugin.version ?? "",
          state: this.i18n.t(`plugin.state.${plugin.state}`),
        })
      : "";
  });

  protected readonly checkViews = computed<CheckView[]>(() => {
    const pluginId = this.id();
    const defs = this.manifest()?.checks ?? {};
    return Object.entries(defs).map(([name, def]) => {
      const ref = `${pluginId}.${name}`;
      const result = this.checks.result(ref);
      return {
        ref,
        label: this.text.checkLabel(pluginId, name, result?.label ?? def.label),
        result,
        details: (result?.details ?? []).map((d) => ({
          term: d.label,
          value: this.info.shorten(d.value),
        })),
        running: this.checks.running().has(ref),
      };
    });
  });

  protected readonly anyCheckRunning = computed(() => this.checkViews().some((c) => c.running));

  protected readonly actionViews = computed<ActionView[]>(() => {
    const pluginId = this.id();
    return Object.entries(this.manifest()?.actions ?? {}).map(([name, def]) => ({
      name,
      label: this.text.actionLabel(pluginId, name, def.label),
      danger: def.danger === true,
      sudo: def.sudo === true,
    }));
  });
  protected readonly hasSudo = computed(() => this.actionViews().some((a) => a.sudo));

  protected readonly pluginSettings = computed(() => this.settings.byPlugin().get(this.id()) ?? []);

  constructor() {
    void Promise.all([
      this.plugins.ensureLoaded(),
      this.checks.ensureLoaded(),
      this.settings.ensureLoaded(),
    ]);
  }

  protected start(action: string): void {
    void this.launcher.start(`${this.id()}.${action}`);
  }

  protected startRef(ref: string): void {
    void this.launcher.start(ref, { quick: true });
  }

  protected actionLabelOf(ref: string): string {
    const [owner, name] = ref.split(".");
    const def = owner && name ? this.plugins.plugin(owner)?.manifest?.actions?.[name] : undefined;
    return owner && name ? this.text.actionLabel(owner, name, def?.label) : ref;
  }

  protected async refreshChecks(): Promise<void> {
    try {
      await this.checks.refresh(this.checkViews().map((c) => c.ref));
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.toast.error(problemText(this.i18n, error));
      } else {
        throw error;
      }
    }
  }
}
