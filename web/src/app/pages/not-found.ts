import { ChangeDetectionStrategy, Component, inject } from "@angular/core";
import { RouterLink } from "@angular/router";
import { ZButton, ZEmptyState } from "zenit-ui";
import { I18n } from "../i18n/i18n";

@Component({
  selector: "dbx-not-found-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, ZEmptyState, ZButton],
  template: `
    <z-empty-state [title]="i18n.t('notFound.title')" headingLevel="1">
      {{ i18n.t("notFound.hint") }}
      <a zEmptyAction zBtn="secondary" routerLink="/">{{ i18n.t("nav.overview") }}</a>
    </z-empty-state>
  `,
})
export class NotFoundPage {
  protected readonly i18n = inject(I18n);
}
