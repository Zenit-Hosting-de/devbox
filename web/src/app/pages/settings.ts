import { ChangeDetectionStrategy, Component, computed, inject, signal } from "@angular/core";
import {
  ZAlert,
  ZAlertAction,
  ZButton,
  ZEmptyState,
  ZField,
  ZInput,
  ZInputGroup,
  ZPageHeader,
  ZPanel,
  ZSkeleton,
} from "zenit-ui";
import type { SettingItem } from "../api/types";
import { problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { Appearance } from "../settings/appearance";
import { SettingsGroup, settingName } from "../settings/settings-group";
import { PluginsStore } from "../stores/plugins.store";
import { SettingsStore } from "../stores/settings.store";

interface GroupView {
  pluginId: string;
  name: string;
  items: SettingItem[];
  visible: ReadonlySet<string> | null;
}

@Component({
  selector: "dbx-settings-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    Appearance,
    SettingsGroup,
    ZAlert,
    ZAlertAction,
    ZButton,
    ZEmptyState,
    ZField,
    ZInput,
    ZInputGroup,
    ZPageHeader,
    ZPanel,
    ZSkeleton,
  ],
  template: `
    <z-page-header [title]="i18n.t('nav.settings')" [sub]="i18n.t('settings.sub')" />

    <div class="z-stack">
      <z-field [label]="i18n.t('settings.search')" for="settings-search">
        <z-input-group icon="search">
          <input
            zInput
            id="settings-search"
            type="search"
            autocomplete="off"
            [value]="query()"
            [placeholder]="i18n.t('settings.searchPlaceholder')"
            (input)="onSearch($event)"
          />
        </z-input-group>
      </z-field>

      @if (error()) {
        <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
          {{ error() }}
          <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="reload()">
            {{ i18n.t("common.retry") }}
          </button>
        </z-alert>
      } @else if (loading()) {
        <z-panel>
          <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
            <z-skeleton width="40%" />
            <z-skeleton width="70%" />
            <z-skeleton width="55%" />
          </div>
        </z-panel>
      } @else {
        @for (group of groups(); track group.pluginId) {
          <z-panel [title]="group.name" headingLevel="2">
            <dbx-settings-group
              [pluginId]="group.pluginId"
              [items]="group.items"
              [visible]="group.visible"
            />
          </z-panel>
        } @empty {
          <z-panel>
            <z-empty-state [title]="i18n.t('settings.noMatch')">
              {{ i18n.t("settings.noMatchHint") }}
            </z-empty-state>
          </z-panel>
        }
      }

      <z-panel [title]="i18n.t('appearance.title')" headingLevel="2">
        <dbx-appearance />
      </z-panel>
    </div>
  `,
})
export class SettingsPage {
  protected readonly i18n = inject(I18n);
  private readonly store = inject(SettingsStore);
  private readonly plugins = inject(PluginsStore);
  private readonly text = inject(PluginText);

  protected readonly query = signal("");

  protected readonly loading = computed(
    () => this.store.state() === "idle" || this.store.state() === "loading",
  );
  protected readonly error = computed(() => {
    const problem = this.store.error();
    return this.store.state() === "error" && problem ? problemText(this.i18n, problem) : "";
  });

  /** Settings grouped by plugin (translated name), filtered by key and label. */
  protected readonly groups = computed<GroupView[]>(() => {
    const query = this.query().trim().toLowerCase();
    const groups: GroupView[] = [];
    for (const [pluginId, items] of this.store.byPlugin()) {
      const plugin = this.plugins.plugin(pluginId);
      const name = plugin ? this.text.pluginName(plugin) : pluginId;
      if (!query) {
        groups.push({ pluginId, name, items, visible: null });
        continue;
      }
      const matching = new Set(
        items
          .filter((item) => {
            const label = this.text.settingLabel(pluginId, settingName(item), item.field.label);
            return item.key.toLowerCase().includes(query) || label.toLowerCase().includes(query);
          })
          .map((item) => settingName(item)),
      );
      if (matching.size > 0) {
        groups.push({ pluginId, name, items, visible: matching });
      }
    }
    return groups.sort((a, b) => a.name.localeCompare(b.name, this.i18n.locale()));
  });

  constructor() {
    void this.plugins.ensureLoaded();
    void this.store.ensureLoaded();
  }

  protected onSearch(event: Event): void {
    this.query.set((event.target as HTMLInputElement).value);
  }

  protected reload(): void {
    void this.store.reload();
  }
}
