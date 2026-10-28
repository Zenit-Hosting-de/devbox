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
  ZRow,
  ZRowMain,
  ZRows,
  ZRowsHead,
  ZSkeleton,
} from "zenit-ui";
import type { PluginDetail } from "../api/types";
import { pluginBadge, problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { PluginsStore } from "../stores/plugins.store";

@Component({
  selector: "dbx-plugins-page",
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
    ZRow,
    ZRowMain,
    ZRows,
    ZRowsHead,
    ZSkeleton,
  ],
  template: `
    <z-page-header
      [title]="i18n.t('nav.plugins')"
      [sub]="i18n.t('plugins.count', { count: plugins.plugins().length })"
    />
    @if (error()) {
      <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
        {{ error() }}
        <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="reload()">
          {{ i18n.t("common.retry") }}
        </button>
      </z-alert>
    } @else {
      <z-panel flush [busy]="plugins.state() !== 'ready'">
        @if (plugins.state() !== "ready") {
          <div class="z-stack loading" role="status" [attr.aria-label]="i18n.t('common.loading')">
            <z-skeleton width="60%" />
            <z-skeleton width="80%" />
            <z-skeleton width="50%" />
          </div>
        } @else if (rows().length === 0) {
          <z-empty-state [title]="i18n.t('plugins.empty')">{{
            i18n.t("plugins.emptyHint")
          }}</z-empty-state>
        } @else {
          <z-rows columns="minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) minmax(0, 2fr) var(--icon)">
            <z-rows-head>
              <span>{{ i18n.t("plugins.name") }}</span>
              <span>{{ i18n.t("plugins.version") }}</span>
              <span>{{ i18n.t("plugins.state") }}</span>
              <span>{{ i18n.t("plugins.reasons") }}</span>
              <span></span>
            </z-rows-head>
            @for (row of rows(); track row.plugin.id) {
              <a zRow [routerLink]="['/plugins', row.plugin.id]">
                <z-row-main [title]="row.name" [meta]="row.description" />
                <span class="z-mono">{{ row.plugin.version }}</span>
                <span>
                  <z-badge [status]="pluginBadge(row.plugin.state)" dot>
                    {{ i18n.t("plugin.state." + row.plugin.state) }}
                  </z-badge>
                </span>
                <span class="z-muted">{{ row.reasons }}</span>
                <z-icon name="chevron_right" />
              </a>
            }
          </z-rows>
        }
      </z-panel>
    }
  `,
  styles: `
    .loading {
      padding: var(--space-4);
    }
  `,
})
export class PluginsPage {
  protected readonly i18n = inject(I18n);
  protected readonly plugins = inject(PluginsStore);
  private readonly text = inject(PluginText);

  protected readonly pluginBadge = pluginBadge;

  protected readonly error = computed(() => {
    const problem = this.plugins.error();
    return this.plugins.state() === "error" && problem ? problemText(this.i18n, problem) : "";
  });

  protected readonly rows = computed(() =>
    this.plugins
      .plugins()
      .map((plugin: PluginDetail) => ({
        plugin,
        name: this.text.pluginName(plugin),
        description: this.text.pluginDescription(plugin) ?? "",
        reasons: plugin.reasons
          .map((r) =>
            r.detail
              ? `${this.i18n.t(`reason.${r.code}`)} (${r.detail})`
              : this.i18n.t(`reason.${r.code}`),
          )
          .join(", "),
      }))
      .sort((a, b) => a.name.localeCompare(b.name, this.i18n.locale())),
  );

  constructor() {
    void this.plugins.ensureLoaded();
  }

  protected reload(): void {
    void this.plugins.reload();
  }
}
