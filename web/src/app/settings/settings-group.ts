import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  signal,
  viewChild,
} from "@angular/core";
import { ZButton, ZToast } from "zenit-ui";
import { ApiProblem } from "../api/problem";
import type { SettingItem } from "../api/types";
import { fieldErrorText, problemText, profileName } from "../core/status";
import { I18n } from "../i18n/i18n";
import { sameValue, type FormValues } from "../schema-form/field";
import { SchemaForm, initialValues, type FieldBadge } from "../schema-form/schema-form";
import { SettingsStore } from "../stores/settings.store";

/** Name of a setting inside its plugin: `java.installDir` -> `installDir`. */
export function settingName(item: Pick<SettingItem, "key" | "plugin">): string {
  return item.key.slice(item.plugin.length + 1);
}

/**
 * The settings of one plugin as a form with save and reset. Used on the settings page and on
 * the plugin page. Only changed keys are sent; the core validates all of them before it
 * writes any.
 */
@Component({
  selector: "dbx-settings-group",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SchemaForm, ZButton],
  template: `
    <dbx-schema-form
      [fields]="visibleFields()"
      [(values)]="draft"
      [pluginId]="pluginId()"
      scope="settings"
      [idPrefix]="'set-' + pluginId()"
      [locked]="locked()"
      [errors]="serverErrors()"
      [badges]="badges()"
      [resettable]="resettable()"
      (resetField)="reset($event)"
    />
    @if (generalError()) {
      <p class="z-field__error" role="alert">{{ generalError() }}</p>
    }
    <div class="actions z-cluster">
      <button
        zBtn="primary"
        type="button"
        [disabled]="changed().length === 0"
        [loading]="saving()"
        (click)="save()"
      >
        {{ i18n.t("settings.save") }}
      </button>
      <button
        zBtn="ghost"
        type="button"
        [disabled]="changed().length === 0 || saving()"
        (click)="discard()"
      >
        {{ i18n.t("settings.discard") }}
      </button>
      @if (changed().length > 0) {
        <span class="z-muted" aria-live="polite">
          {{ i18n.t("settings.changes", { count: changed().length }) }}
        </span>
      }
    </div>
  `,
  styles: `
    .actions {
      margin-block-start: var(--space-5);
    }
  `,
})
export class SettingsGroup {
  protected readonly i18n = inject(I18n);
  private readonly store = inject(SettingsStore);
  private readonly toast = inject(ZToast);

  readonly pluginId = input.required<string>();
  readonly items = input.required<readonly SettingItem[]>();
  /** Names to show (a search filter); null shows all. Hidden fields keep their edits. */
  readonly visible = input<ReadonlySet<string> | null>(null);

  private readonly form = viewChild.required(SchemaForm);

  protected readonly fields = computed(() =>
    Object.fromEntries(this.items().map((item) => [settingName(item), item.field])),
  );

  protected readonly visibleFields = computed(() => {
    const visible = this.visible();
    const all = this.fields();
    return visible
      ? Object.fromEntries(Object.entries(all).filter(([name]) => visible.has(name)))
      : all;
  });

  private readonly baseline = computed<FormValues>(() => {
    const stored: Record<string, unknown> = {};
    for (const item of this.items()) {
      if (item.value !== undefined) {
        stored[settingName(item)] = item.value;
      }
    }
    return initialValues(this.fields(), stored);
  });

  /** The values in the form; reset to the stored ones whenever the settings reload. */
  protected readonly draft = linkedSignal<FormValues>(() => this.baseline());

  protected readonly serverErrors = signal<Readonly<Record<string, string>>>({});
  protected readonly generalError = signal("");
  protected readonly saving = signal(false);

  protected readonly locked = computed(() => {
    const out: Record<string, string> = {};
    for (const item of this.items()) {
      if (item.locked) {
        out[settingName(item)] = profileName(item.lockedBy);
      }
    }
    return out;
  });

  protected readonly badges = computed(() => {
    const out: Record<string, FieldBadge> = {};
    for (const item of this.items()) {
      if (item.origin) {
        out[settingName(item)] = originBadge(this.i18n, item.origin);
      }
    }
    return out;
  });

  protected readonly resettable = computed(() => {
    const out: Record<string, boolean> = {};
    for (const item of this.items()) {
      out[settingName(item)] = item.origin === "user" && !item.locked;
    }
    return out;
  });

  /** Names whose value differs from the stored one. */
  protected readonly changed = computed(() => {
    const draft = this.draft();
    const base = this.baseline();
    return Object.keys(this.fields()).filter((name) => {
      if (this.locked()[name]) {
        return false;
      }
      return !sameValue(draft[name], base[name]);
    });
  });

  protected discard(): void {
    this.draft.set(this.baseline());
    this.serverErrors.set({});
    this.generalError.set("");
  }

  protected async save(): Promise<void> {
    const names = this.changed();
    if (names.length === 0 || !this.form().validate(names)) {
      return;
    }
    const draft = this.draft();
    const patch: Record<string, unknown> = {};
    for (const name of names) {
      const item = this.itemOf(name);
      if (item) {
        patch[item.key] = draft[name] === undefined ? null : draft[name];
      }
    }
    await this.send(patch, this.i18n.t("settings.saved"));
  }

  protected async reset(name: string): Promise<void> {
    const item = this.itemOf(name);
    if (item) {
      await this.send({ [item.key]: null }, this.i18n.t("settings.resetDone"));
    }
  }

  private itemOf(name: string): SettingItem | undefined {
    return this.items().find((item) => settingName(item) === name);
  }

  private async send(patch: Record<string, unknown>, success: string): Promise<void> {
    this.saving.set(true);
    this.serverErrors.set({});
    this.generalError.set("");
    try {
      await this.store.patch(patch);
      this.toast.success(success);
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.showProblem(error, Object.keys(patch));
      } else {
        throw error;
      }
    } finally {
      this.saving.set(false);
    }
  }

  /** Maps field errors by pointer `/<key>`; a lock without a pointer lands on the first key. */
  private showProblem(problem: ApiProblem, keys: readonly string[]): void {
    const byName: Record<string, string> = {};
    for (const error of problem.errors) {
      const key = error.pointer.replace(/^\//, "");
      const item = this.items().find((i) => i.key === key);
      if (item) {
        byName[settingName(item)] = fieldErrorText(this.i18n, error);
      }
    }
    if (Object.keys(byName).length === 0 && problem.code === "setting-locked") {
      const item = this.items().find((i) => keys.includes(i.key));
      if (item) {
        byName[settingName(item)] = this.i18n.t("problem.setting-locked");
      }
    }
    this.serverErrors.set(byName);
    if (Object.keys(byName).length === 0) {
      this.generalError.set(problemText(this.i18n, problem));
    }
  }
}

/** The badge that names the layer a value comes from. */
export function originBadge(i18n: I18n, origin: string): FieldBadge {
  if (origin.startsWith("profile:")) {
    return { text: i18n.t("origin.profile", { name: profileName(origin) }), status: "warning" };
  }
  const key = `origin.${origin}`;
  return {
    text: i18n.has(key) ? i18n.t(key) : origin,
    status: origin === "user" ? "info" : "neutral",
  };
}
