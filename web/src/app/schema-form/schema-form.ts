import { NgTemplateOutlet } from "@angular/common";
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  model,
  output,
  signal,
} from "@angular/core";
import {
  ZBadge,
  ZButton,
  ZDisclosure,
  ZField,
  ZIcon,
  ZInput,
  ZSegment,
  ZSelect,
  ZSetting,
  ZToggle,
} from "zenit-ui";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { ChecksStore } from "../stores/checks.store";
import {
  fieldDefault,
  isPlaceholder,
  isRequired,
  resolveOptionsFrom,
  validateField,
  type FieldError,
  type FormField,
  type FormValues,
} from "./field";

/** Largest enum that is shown as a segment; bigger ones become a select. */
export const SEGMENT_MAX_OPTIONS = 5;

export interface FieldBadge {
  text: string;
  status?: "neutral" | "success" | "warning" | "danger" | "info";
}

interface Choice {
  value: string;
  label: string;
}

/**
 * Builds a form from field schemas (plugin.v1.json $defs/field). It knows no plugin: labels and
 * help texts come from the plugin translations with the manifest text as fallback.
 */
@Component({
  selector: "dbx-schema-form",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    NgTemplateOutlet,
    ZBadge,
    ZButton,
    ZDisclosure,
    ZField,
    ZIcon,
    ZInput,
    ZSegment,
    ZSelect,
    ZSetting,
    ZToggle,
  ],
  template: `
    <div class="z-stack">
      @for (name of basicNames(); track name) {
        <ng-container *ngTemplateOutlet="fieldTpl; context: { $implicit: name }" />
      }
      @if (advancedNames().length > 0) {
        <z-disclosure [title]="i18n.t('form.moreOptions')" [(open)]="advancedOpen">
          <div class="z-stack">
            @for (name of advancedNames(); track name) {
              <ng-container *ngTemplateOutlet="fieldTpl; context: { $implicit: name }" />
            }
          </div>
        </z-disclosure>
      }
    </div>

    <ng-template #fieldTpl let-name>
      @let field = fields()[name];
      @if (field) {
        @if (field.type === "boolean") {
          <z-setting
            [title]="labelOf(name)"
            [description]="descriptionOf(name)"
            [titleId]="idFor(name) + '-title'"
          >
            <z-toggle
              [checked]="valueOf(name) === true"
              (checkedChange)="setValue(name, $event)"
              [disabled]="isLocked(name)"
              [ariaLabelledby]="idFor(name) + '-title'"
              [ariaDescribedby]="idFor(name) + '-meta'"
            />
          </z-setting>
          <ng-container *ngTemplateOutlet="metaTpl; context: { $implicit: name }" />
        } @else {
          <z-field
            [label]="labelOf(name)"
            [for]="idFor(name)"
            [hint]="hintOf(name)"
            [error]="errorOf(name)"
          >
            <ng-container *ngTemplateOutlet="metaTpl; context: { $implicit: name }" />
            @switch (kindOf(field)) {
              @case ("secret") {
                <input
                  zInput
                  type="password"
                  autocomplete="new-password"
                  [id]="idFor(name)"
                  [invalid]="hasError(name)"
                  [readOnly]="isLocked(name)"
                  [value]="''"
                  [placeholder]="i18n.t('form.secretPlaceholder')"
                  (input)="onText(name, $event)"
                />
              }
              @case ("segment") {
                <z-segment
                  [options]="choicesOf(field)"
                  [value]="currentChoice(name)"
                  (valueChange)="onChoice(name, field, $event)"
                  [ariaLabel]="labelOf(name)"
                  [disabled]="isLocked(name)"
                />
              }
              @case ("select") {
                <z-select>
                  <select
                    [id]="idFor(name)"
                    [disabled]="isLocked(name)"
                    (change)="onSelect(name, field, $event)"
                  >
                    @if (!isRequiredField(name)) {
                      <option value="" [selected]="currentChoice(name) === ''">
                        {{ i18n.t("form.unset") }}
                      </option>
                    }
                    @for (choice of choicesOf(field); track choice.value) {
                      <option
                        [value]="choice.value"
                        [selected]="choice.value === currentChoice(name)"
                      >
                        {{ choice.label }}
                      </option>
                    }
                  </select>
                </z-select>
              }
              @case ("options") {
                <z-select>
                  <select
                    [id]="idFor(name)"
                    [disabled]="isLocked(name)"
                    (change)="onSelect(name, field, $event)"
                  >
                    <option value="" [selected]="currentChoice(name) === ''">
                      {{ i18n.t("form.choose") }}
                    </option>
                    @for (choice of optionChoices(name, field); track choice.value) {
                      <option
                        [value]="choice.value"
                        [selected]="choice.value === currentChoice(name)"
                      >
                        {{ choice.label }}
                      </option>
                    }
                  </select>
                </z-select>
              }
              @case ("number") {
                <input
                  zInput
                  mono
                  type="number"
                  [id]="idFor(name)"
                  [invalid]="hasError(name)"
                  [readOnly]="isLocked(name)"
                  [attr.min]="field.minimum ?? field.exclusiveMinimum ?? null"
                  [attr.max]="field.maximum ?? field.exclusiveMaximum ?? null"
                  [attr.step]="field.type === 'integer' ? 1 : 'any'"
                  [value]="numberText(name)"
                  (input)="onNumber(name, $event)"
                  (blur)="touch(name)"
                />
              }
              @case ("list") {
                <div class="list">
                  @for (item of listOf(name); track $index) {
                    <div class="list-row">
                      <input
                        zInput
                        mono
                        [id]="idFor(name) + '-' + $index"
                        [attr.aria-label]="
                          i18n.t('form.itemLabel', { label: labelOf(name), n: $index + 1 })
                        "
                        [invalid]="itemInvalid(name, $index)"
                        [readOnly]="isLocked(name)"
                        [value]="item"
                        (input)="onItem(name, $index, $event)"
                        (blur)="touch(name)"
                      />
                      <button
                        zBtn="ghost"
                        size="sm"
                        iconOnly
                        type="button"
                        [disabled]="isLocked(name)"
                        [attr.aria-label]="i18n.t('form.removeItem', { n: $index + 1 })"
                        (click)="removeItem(name, $index)"
                      >
                        <z-icon name="close" />
                      </button>
                    </div>
                  }
                  <div>
                    <button
                      zBtn="secondary"
                      size="sm"
                      type="button"
                      [disabled]="isLocked(name)"
                      (click)="addItem(name)"
                    >
                      <z-icon name="add" />{{ i18n.t("form.addItem") }}
                    </button>
                  </div>
                </div>
              }
              @default {
                <input
                  zInput
                  [mono]="field.format === 'path' || field.format === 'uri'"
                  [attr.inputmode]="field.format === 'uri' ? 'url' : null"
                  type="text"
                  [id]="idFor(name)"
                  [invalid]="hasError(name)"
                  [readOnly]="isLocked(name)"
                  [placeholder]="placeholderOf(field)"
                  [value]="textOf(name)"
                  (input)="onText(name, $event)"
                  (blur)="touch(name)"
                />
              }
            }
          </z-field>
        }
        @if (errorOf(name) && field.type === "boolean") {
          <span class="z-field__error">{{ errorOf(name) }}</span>
        }
      }
    </ng-template>

    <ng-template #metaTpl let-name>
      @let badge = badges()[name];
      @let lockedBy = locked()[name];
      @if (badge || lockedBy || resettable()[name]) {
        <div class="meta" [id]="idFor(name) + '-meta'">
          @if (badge) {
            <z-badge [status]="badge.status ?? 'neutral'">{{ badge.text }}</z-badge>
          }
          @if (lockedBy) {
            <z-badge status="warning" dot>{{
              i18n.t("form.lockedBy", { name: lockedBy })
            }}</z-badge>
          }
          @if (resettable()[name] && !lockedBy) {
            <button zBtn="ghost" size="sm" type="button" (click)="resetField.emit(name)">
              <z-icon name="restart_alt" />{{ i18n.t("form.reset") }}
            </button>
          }
        </div>
      }
    </ng-template>
  `,
  styles: `
    .list {
      display: grid;
      gap: var(--space-2);
    }
    .list-row {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: var(--space-2);
      align-items: center;
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-2);
      margin-block-end: var(--space-2);
    }
  `,
})
export class SchemaForm {
  protected readonly i18n = inject(I18n);
  private readonly text = inject(PluginText);
  private readonly checks = inject(ChecksStore);

  /** Field schemas by name, in display order. */
  readonly fields = input.required<Readonly<Record<string, FormField>>>();
  /** Typed values by name (two-way). */
  readonly values = model<FormValues>({});
  /** Plugin that owns the fields; its translation file supplies labels and help. */
  readonly pluginId = input.required<string>();
  /** Prefix of the translation keys: `settings` or `actions.<action>.params`. */
  readonly scope = input.required<string>();
  /** Names of locked fields mapped to the profile that locks them. */
  readonly locked = input<Readonly<Record<string, string>>>({});
  /** Server-side errors by field name, already translated. */
  readonly errors = input<Readonly<Record<string, string>>>({});
  /** Small badges shown above a field (the origin layer of a setting). */
  readonly badges = input<Readonly<Record<string, FieldBadge>>>({});
  /** Fields that offer a reset button. */
  readonly resettable = input<Readonly<Record<string, boolean>>>({});
  readonly idPrefix = input("field");

  readonly resetField = output<string>();

  protected readonly advancedOpen = signal(false);
  private readonly touched = signal<ReadonlySet<string>>(new Set());

  private readonly names = computed(() => Object.keys(this.fields()));
  protected readonly basicNames = computed(() =>
    this.names().filter((n) => this.fields()[n]?.advanced !== true),
  );
  protected readonly advancedNames = computed(() =>
    this.names().filter((n) => this.fields()[n]?.advanced === true),
  );

  /** Client-side errors of the fields that were touched or validated. */
  private readonly clientErrors = computed(() => {
    const out = new Map<string, FieldError>();
    const values = this.values();
    for (const name of this.touched()) {
      const field = this.fields()[name];
      if (!field || this.locked()[name]) {
        continue;
      }
      const error = validateField(field, values[name], isRequired(field));
      if (error) {
        out.set(name, error);
      }
    }
    return out;
  });

  constructor() {
    // An error inside the collapsed options must not stay hidden.
    effect(() => {
      const hidden = this.advancedNames().some(
        (n) => this.errors()[n] !== undefined || this.clientErrors().has(n),
      );
      if (hidden) {
        this.advancedOpen.set(true);
      }
    });
  }

  /**
   * Validates the fields (all, or the named ones) and shows their errors.
   * Locked fields are skipped. Returns true when everything is valid.
   */
  validate(only?: readonly string[]): boolean {
    const names = only ?? this.names();
    this.touched.update((set) => new Set([...set, ...names]));
    const values = this.values();
    return names.every((name) => {
      const field = this.fields()[name];
      if (!field || this.locked()[name]) {
        return true;
      }
      return validateField(field, values[name], isRequired(field)) === null;
    });
  }

  // ---- texts -------------------------------------------------------------

  protected idFor(name: string): string {
    return `${this.idPrefix()}-${name}`;
  }

  protected labelOf(name: string): string {
    const field = this.fields()[name];
    const label = this.text.text(
      this.pluginId(),
      `${this.scope()}.${name}.label`,
      field?.label ?? name,
    );
    return this.isRequiredField(name) ? `${label} *` : label;
  }

  protected descriptionOf(name: string): string {
    return (
      this.text.optional(
        this.pluginId(),
        `${this.scope()}.${name}.help`,
        this.fields()[name]?.help,
      ) ?? ""
    );
  }

  protected hintOf(name: string): string {
    return this.descriptionOf(name);
  }

  protected errorOf(name: string): string {
    const server = this.errors()[name];
    if (server) {
      return server;
    }
    const error = this.clientErrors().get(name);
    if (!error) {
      return "";
    }
    const message = this.i18n.t(`form.error.${error.code}`, error.params);
    return error.index === undefined
      ? message
      : this.i18n.t("form.error.item", { n: error.index + 1, message });
  }

  protected hasError(name: string): boolean {
    return this.errorOf(name) !== "";
  }

  protected itemInvalid(name: string, index: number): boolean {
    return this.clientErrors().get(name)?.index === index || this.errors()[name] !== undefined;
  }

  protected isLocked(name: string): boolean {
    return this.locked()[name] !== undefined;
  }

  protected isRequiredField(name: string): boolean {
    const field = this.fields()[name];
    return field !== undefined && isRequired(field) && !this.isLocked(name);
  }

  // ---- control kinds -----------------------------------------------------

  protected kindOf(
    field: FormField,
  ): "secret" | "segment" | "select" | "options" | "number" | "list" | "text" {
    if (field.secret) {
      return "secret";
    }
    if (field.type === "array") {
      return "list";
    }
    if (field.optionsFrom) {
      return "options";
    }
    if (field.enum && field.enum.length > 0) {
      return field.enum.length <= SEGMENT_MAX_OPTIONS ? "segment" : "select";
    }
    if (field.type === "integer" || field.type === "number") {
      return "number";
    }
    return "text";
  }

  protected choicesOf(field: FormField): Choice[] {
    return (field.enum ?? []).map((v) => ({ value: String(v), label: String(v) }));
  }

  protected optionChoices(name: string, field: FormField): Choice[] {
    const found = field.optionsFrom
      ? (resolveOptionsFrom(field.optionsFrom, this.checks.results()) ?? [])
      : [];
    const choices = found.map((v) => ({ value: String(v), label: String(v) }));
    const current = this.currentChoice(name);
    if (current !== "" && !choices.some((c) => c.value === current)) {
      choices.unshift({ value: current, label: current });
    }
    return choices;
  }

  protected currentChoice(name: string): string {
    const value = this.valueOf(name);
    return value === undefined || value === null ? "" : String(value);
  }

  protected placeholderOf(field: FormField): string {
    if (typeof field.default === "string" && field.default !== "") {
      return field.default;
    }
    const example = field.examples?.[0];
    return typeof example === "string" ? example : "";
  }

  // ---- values ------------------------------------------------------------

  protected valueOf(name: string): unknown {
    return this.values()[name];
  }

  protected textOf(name: string): string {
    const value = this.valueOf(name);
    return typeof value === "string" ? value : value === undefined ? "" : String(value);
  }

  protected numberText(name: string): string {
    const value = this.valueOf(name);
    return typeof value === "number" ? String(value) : "";
  }

  protected listOf(name: string): string[] {
    const value = this.valueOf(name);
    return Array.isArray(value) ? value.map(String) : [];
  }

  protected touch(name: string): void {
    this.touched.update((set) => (set.has(name) ? set : new Set(set).add(name)));
  }

  protected setValue(name: string, value: unknown): void {
    this.values.update((current) => {
      const rest = Object.fromEntries(Object.entries(current).filter(([key]) => key !== name));
      return value === undefined ? rest : { ...rest, [name]: value };
    });
    this.touch(name);
  }

  protected onText(name: string, event: Event): void {
    const raw = (event.target as HTMLInputElement).value;
    this.setValue(name, raw === "" ? undefined : raw);
  }

  protected onNumber(name: string, event: Event): void {
    const raw = (event.target as HTMLInputElement).value;
    const parsed = raw === "" ? undefined : Number(raw);
    this.setValue(name, parsed !== undefined && Number.isNaN(parsed) ? undefined : parsed);
  }

  protected onChoice(name: string, field: FormField, raw: string): void {
    if (raw === "") {
      this.setValue(name, undefined);
      return;
    }
    const typed = (field.enum ?? []).find((v) => String(v) === raw);
    this.setValue(name, typed ?? this.coerce(field, raw));
  }

  protected onSelect(name: string, field: FormField, event: Event): void {
    this.onChoice(name, field, (event.target as HTMLSelectElement).value);
  }

  private coerce(field: FormField, raw: string): unknown {
    if (field.type === "integer" || field.type === "number") {
      const n = Number(raw);
      return Number.isNaN(n) ? raw : n;
    }
    return raw;
  }

  protected addItem(name: string): void {
    this.setValue(name, [...this.listOf(name), ""]);
  }

  protected removeItem(name: string, index: number): void {
    const next = this.listOf(name).filter((_, i) => i !== index);
    this.setValue(name, next.length > 0 ? next : undefined);
  }

  protected onItem(name: string, index: number, event: Event): void {
    const raw = (event.target as HTMLInputElement).value;
    const next = this.listOf(name);
    next[index] = raw;
    this.setValue(name, next);
  }
}

/** The values to prefill a form with: the stored value, else the (resolved) default. */
export function initialValues(
  fields: Readonly<Record<string, FormField>>,
  stored: Readonly<Record<string, unknown>>,
): FormValues {
  const out: FormValues = {};
  for (const [name, field] of Object.entries(fields)) {
    if (field.secret) {
      continue;
    }
    const value = stored[name] !== undefined ? stored[name] : fieldDefault(field);
    if (value !== undefined && !isPlaceholder(value)) {
      out[name] = value;
    }
  }
  return out;
}
