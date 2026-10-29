import { ChangeDetectionStrategy, Component, inject, input } from "@angular/core";
import { RouterLink } from "@angular/router";
import { ZTab, ZTabs } from "zenit-ui";
import { I18n } from "../i18n/i18n";

/** Tab bar of the plugin area: the installed plugins and their sources. */
@Component({
  selector: "dbx-plugin-tabs",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, ZTab, ZTabs],
  template: `
    <nav zTabs [attr.aria-label]="i18n.t('plugins.tabs')">
      <a zTab routerLink="/plugins" [active]="active() === 'installed'">
        {{ i18n.t("plugins.tab.installed") }}
      </a>
      <a zTab routerLink="/plugins/sources" [active]="active() === 'sources'">
        {{ i18n.t("plugins.tab.sources") }}
      </a>
    </nav>
  `,
  styles: `
    :host {
      display: block;
      margin-block-end: var(--space-4);
    }
  `,
})
export class PluginTabs {
  protected readonly i18n = inject(I18n);
  readonly active = input.required<"installed" | "sources">();
}
