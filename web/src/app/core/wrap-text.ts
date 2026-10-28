import { ChangeDetectionStrategy, Component, computed, inject, input } from "@angular/core";
import { ZTooltip } from "zenit-ui";
import { InfoStore } from "../stores/info.store";

/**
 * Text that comes from the core or a plugin and may hold long paths. The home directory is
 * shown as `~`, the text wraps anywhere instead of breaking the layout, and the full text is
 * the tooltip whenever it was shortened.
 */
@Component({
  selector: "dbx-wrap-text",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ZTooltip],
  template: `<span [zTooltip]="tooltip()" [attr.tabindex]="tooltip() ? 0 : null">{{
    shown()
  }}</span>`,
  styles: `
    :host {
      display: inline-block;
      max-inline-size: 100%;
      min-inline-size: 0;
      overflow-wrap: anywhere;
    }
  `,
})
export class WrapText {
  readonly text = input.required<string>();

  private readonly info = inject(InfoStore);

  protected readonly shown = computed(() => this.info.shorten(this.text()));
  protected readonly tooltip = computed(() => (this.shown() === this.text() ? "" : this.text()));
}
