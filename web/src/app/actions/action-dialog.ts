import { DIALOG_DATA, DialogRef } from "@angular/cdk/dialog";
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
  viewChild,
} from "@angular/core";
import {
  ZAlert,
  ZButton,
  ZCheckbox,
  ZDialogActions,
  ZDialogLayout,
  ZIcon,
  ZSetting,
  ZToggle,
} from "zenit-ui";
import { ApiProblem } from "../api/problem";
import type { PluginDetail, Session, SessionCreate } from "../api/types";
import { fieldErrorText, problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginText } from "../i18n/plugin-text";
import { isEmpty, type FormField, type FormValues } from "../schema-form/field";
import { SchemaForm, initialValues } from "../schema-form/schema-form";
import { DockStore } from "../stores/dock.store";
import { SessionsStore } from "../stores/sessions.store";

export interface ActionDialogData {
  plugin: PluginDetail;
  action: string;
}

/** Drops empty values so the core applies its own defaults. */
export function cleanParams(
  values: FormValues,
  fields: Readonly<Record<string, FormField>>,
): FormValues {
  const out: FormValues = {};
  for (const name of Object.keys(fields)) {
    const value = values[name];
    if (Array.isArray(value)) {
      const items = value.filter((v) => v !== "");
      if (items.length > 0) {
        out[name] = items;
      }
    } else if (!isEmpty(value)) {
      out[name] = value;
    }
  }
  return out;
}

/**
 * Starts an action: a form for its parameters (prefilled from the resolved defaults), a dry-run
 * switch and, for actions with a `confirm` text, an explicit confirmation. The result is the
 * created session, which opens in the dock.
 */
@Component({
  selector: "dbx-action-dialog",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    SchemaForm,
    ZAlert,
    ZButton,
    ZCheckbox,
    ZDialogActions,
    ZDialogLayout,
    ZIcon,
    ZSetting,
    ZToggle,
  ],
  template: `
    <z-dialog [title]="title()">
      <div class="z-stack">
        @if (description()) {
          <p class="z-muted">{{ description() }}</p>
        }
        @if (sudo()) {
          <p class="z-muted"><z-icon name="lock" size="sm" /> {{ i18n.t("action.sudoHint") }}</p>
        }
        @if (hasParams()) {
          <dbx-schema-form
            [fields]="fields()"
            [(values)]="values"
            [pluginId]="data.plugin.id"
            [scope]="'actions.' + data.action + '.params'"
            [errors]="fieldErrors()"
            idPrefix="param"
          />
        }
        <z-setting
          [title]="i18n.t('action.dryRun')"
          [description]="i18n.t('action.dryRunHelp')"
          titleId="dry-run-title"
        >
          <z-toggle [(checked)]="dryRun" ariaLabelledby="dry-run-title" />
        </z-setting>
        @if (needsConfirm()) {
          <z-alert status="warning" icon="warning" [title]="i18n.t('action.confirmTitle')">
            {{ confirmText() }}
          </z-alert>
          <z-checkbox [(checked)]="confirmed" ariaDescribedby="confirm-error">
            {{ i18n.t("action.confirmCheckbox") }}
          </z-checkbox>
          @if (confirmError()) {
            <span id="confirm-error" class="z-field__error" role="alert">{{ confirmError() }}</span>
          }
        }
        @if (generalError()) {
          <z-alert status="danger" icon="error">{{ generalError() }}</z-alert>
        }
      </div>
      <ng-container zDialogActions>
        <button zBtn="ghost" type="button" (click)="ref.close()">
          {{ i18n.t("common.cancel") }}
        </button>
        <button
          [zBtn]="danger() ? 'danger' : 'primary'"
          type="button"
          [loading]="busy()"
          (click)="submit()"
        >
          {{ dryRun() ? i18n.t("action.startDry") : i18n.t("action.start") }}
        </button>
      </ng-container>
    </z-dialog>
  `,
})
export class ActionDialog {
  protected readonly data = inject<ActionDialogData>(DIALOG_DATA);
  protected readonly ref = inject<DialogRef<Session | undefined>>(DialogRef);
  protected readonly i18n = inject(I18n);
  private readonly text = inject(PluginText);
  private readonly sessions = inject(SessionsStore);
  private readonly dock = inject(DockStore);

  private readonly form = viewChild(SchemaForm);

  private readonly manifestAction = computed(
    () => this.data.plugin.manifest?.actions?.[this.data.action],
  );
  protected readonly fields = computed<Readonly<Record<string, FormField>>>(
    () => this.manifestAction()?.params ?? {},
  );
  protected readonly hasParams = computed(() => Object.keys(this.fields()).length > 0);
  protected readonly title = computed(() =>
    this.text.actionLabel(this.data.plugin.id, this.data.action, this.manifestAction()?.label),
  );
  protected readonly description = computed(() =>
    this.text.actionDescription(
      this.data.plugin.id,
      this.data.action,
      this.manifestAction()?.description,
    ),
  );
  protected readonly danger = computed(() => this.manifestAction()?.danger === true);
  protected readonly sudo = computed(() => this.manifestAction()?.sudo === true);
  protected readonly confirmText = computed(() =>
    this.text.actionConfirm(this.data.plugin.id, this.data.action, this.manifestAction()?.confirm),
  );

  protected readonly values = signal<FormValues>(
    initialValues(
      this.data.plugin.manifest?.actions?.[this.data.action]?.params ?? {},
      this.data.plugin.resolvedDefaults?.[this.data.action] ?? {},
    ),
  );
  protected readonly dryRun = signal(false);
  protected readonly confirmed = signal(false);
  protected readonly busy = signal(false);
  protected readonly fieldErrors = signal<Readonly<Record<string, string>>>({});
  protected readonly confirmError = signal("");
  protected readonly generalError = signal("");

  /** The confirmation is asked for real runs only; a dry run changes nothing. */
  protected readonly needsConfirm = computed(() => this.confirmText() !== "" && !this.dryRun());

  protected async submit(): Promise<void> {
    this.fieldErrors.set({});
    this.confirmError.set("");
    this.generalError.set("");
    if (this.hasParams() && this.form()?.validate() === false) {
      return;
    }
    if (this.needsConfirm() && !this.confirmed()) {
      this.confirmError.set(this.i18n.t("action.confirmRequired"));
      return;
    }
    const size = this.dock.size();
    const params = cleanParams(this.values(), this.fields());
    const body: SessionCreate = {
      action: `${this.data.plugin.id}.${this.data.action}`,
      dryRun: this.dryRun(),
      cols: size.cols,
      rows: size.rows,
      ...(this.hasParams() ? { params } : {}),
      ...(this.needsConfirm() ? { yes: true } : {}),
    };
    this.busy.set(true);
    try {
      const session = await this.sessions.create(body);
      this.dock.openNew(session);
      this.ref.close(session);
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.showProblem(error);
      } else {
        throw error;
      }
    } finally {
      this.busy.set(false);
    }
  }

  /** Field errors by pointer `/params/<name>`; `/yes` belongs to the confirmation. */
  private showProblem(problem: ApiProblem): void {
    const fields: Record<string, string> = {};
    let rest = false;
    for (const error of problem.errors) {
      const params = /^\/params\/(.+)$/.exec(error.pointer);
      if (params?.[1] !== undefined && params[1] in this.fields()) {
        fields[params[1]] = fieldErrorText(this.i18n, error);
      } else if (error.pointer === "/yes") {
        this.confirmError.set(this.i18n.t("action.confirmRequired"));
      } else {
        rest = true;
      }
    }
    this.fieldErrors.set(fields);
    if (problem.errors.length === 0 || rest) {
      this.generalError.set(problemText(this.i18n, problem));
    }
  }
}
