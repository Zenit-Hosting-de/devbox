import { ChangeDetectionStrategy, Component, computed, inject } from "@angular/core";
import { ZField, ZSegment, ZTheme, type ZSegmentOption } from "zenit-ui";
import { I18n, LANGUAGES, isLanguage } from "../i18n/i18n";

/** Colour schemes the UI offers; 'system' follows the operating system. */
export const SCHEMES = ["system", "light", "dark", "contrast"] as const;

/**
 * Language and colour scheme of this browser. Both are kept client-side in this milestone
 * (the configured value only supplies the initial choice); persisting them into the
 * configuration file comes with AP-28.
 */
@Component({
  selector: "dbx-appearance",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ZField, ZSegment],
  template: `
    <div class="z-stack">
      <z-field [label]="i18n.t('appearance.language')" [hint]="i18n.t('appearance.clientSide')">
        <z-segment
          [options]="languageOptions"
          [value]="i18n.language()"
          (valueChange)="setLanguage($event)"
          [ariaLabel]="i18n.t('appearance.language')"
        />
      </z-field>
      <z-field [label]="i18n.t('appearance.theme')">
        <z-segment
          [options]="schemeOptions()"
          [value]="theme.scheme()"
          (valueChange)="setScheme($event)"
          [ariaLabel]="i18n.t('appearance.theme')"
        />
      </z-field>
    </div>
  `,
})
export class Appearance {
  protected readonly i18n = inject(I18n);
  protected readonly theme = inject(ZTheme);

  protected readonly languageOptions: ZSegmentOption[] = LANGUAGES.map((l) => ({
    value: l.code,
    label: l.name,
  }));

  protected readonly schemeOptions = computed<ZSegmentOption[]>(() =>
    SCHEMES.map((s) => ({ value: s, label: this.i18n.t(`theme.${s}`) })),
  );

  protected setLanguage(value: string): void {
    if (isLanguage(value)) {
      void this.i18n.setLanguage(value);
    }
  }

  protected setScheme(value: string): void {
    this.theme.setScheme(value);
  }
}
