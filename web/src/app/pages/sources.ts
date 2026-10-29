import { ChangeDetectionStrategy, Component, computed, inject, signal } from "@angular/core";
import { firstValueFrom } from "rxjs";
import {
  ZAlert,
  ZAlertAction,
  ZBadge,
  ZButton,
  ZDialog,
  ZEmptyState,
  ZField,
  ZIcon,
  ZInput,
  ZOptionGroup,
  ZPageHeader,
  ZPanel,
  ZPanelActions,
  ZSkeleton,
  ZSpecList,
  ZToast,
  type ZBadgeStatus,
  type ZOption,
  type ZSpecItem,
} from "zenit-ui";
import { ApiProblem } from "../api/problem";
import type { Repository, RepositoryCreate, RepositoryState } from "../api/types";
import { fieldErrorText, layerName, problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { RepositoriesStore } from "../stores/repositories.store";
import { PluginTabs } from "./plugin-tabs";
import {
  RepositoryReview,
  type RepositoryReviewData,
  type RepositoryReviewResult,
} from "./repository-review";

export const NAME_PATTERN = /^[a-z][a-z0-9-]{1,31}$/;
const SOURCE_PATTERN = /^(git\+https|git\+ssh|git\+file|https|file):\/\/\S+$/;
const COMMIT_PATTERN = /^[0-9a-f]{40}$/;

type Field = "name" | "source" | "ref" | "path" | "trust";
type Trust = "prompt" | "pinned";
export type FieldErrors = Partial<Record<Field, string>>;

/** Client-side checks of the form; the core validates again. Values are the trimmed inputs. */
export function validateSource(
  i18n: I18n,
  values: { name: string; source: string; ref: string; trust: Trust },
): FieldErrors {
  const errors: FieldErrors = {};
  if (values.name === "") {
    errors.name = i18n.t("form.error.required");
  } else if (!NAME_PATTERN.test(values.name)) {
    errors.name = i18n.t("sources.error.name");
  }
  const git = values.source.startsWith("git+");
  if (values.source === "") {
    errors.source = i18n.t("form.error.required");
  } else if (!SOURCE_PATTERN.test(values.source)) {
    errors.source = i18n.t("sources.error.source");
  }
  if (values.ref !== "" && !git && !errors.source) {
    errors.ref = i18n.t("sources.error.refGit");
  }
  if (values.trust === "pinned" && !(git && COMMIT_PATTERN.test(values.ref))) {
    errors.trust = i18n.t("sources.error.pinned");
  }
  return errors;
}

const STATE_BADGE: Record<RepositoryState, ZBadgeStatus> = {
  new: "neutral",
  active: "success",
  pending: "warning",
  error: "danger",
  disabled: "neutral",
  unsupported: "neutral",
};

interface RepositoryView {
  repository: Repository;
  badge: ZBadgeStatus;
  state: string;
  facts: ZSpecItem[];
  busy: boolean;
  removable: boolean;
  syncable: boolean;
}

/** The sources of plugins: list, sync, review of a pending revision, add and remove. */
@Component({
  selector: "dbx-sources-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    PluginTabs,
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZField,
    ZIcon,
    ZInput,
    ZOptionGroup,
    ZPageHeader,
    ZPanel,
    ZPanelActions,
    ZSkeleton,
    ZSpecList,
  ],
  template: `
    <z-page-header
      [title]="i18n.t('nav.plugins')"
      [sub]="i18n.t('sources.count', { count: store.repositories().length })"
    >
      <button
        zBtn="primary"
        type="button"
        [attr.aria-expanded]="formOpen()"
        aria-controls="add-source"
        (click)="toggleForm()"
      >
        <z-icon name="add" />{{ i18n.t("sources.add") }}
      </button>
    </z-page-header>
    <dbx-plugin-tabs active="sources" />

    <div class="z-stack">
      @if (formOpen()) {
        <z-panel
          id="add-source"
          [title]="i18n.t('sources.addTitle')"
          headingLevel="2"
          [busy]="store.adding()"
        >
          <form class="z-stack" novalidate (submit)="submit($event)">
            <z-field
              [label]="i18n.t('sources.field.name')"
              for="src-name"
              [hint]="i18n.t('sources.field.nameHint')"
              [error]="errors().name ?? ''"
            >
              <input
                zInput
                mono
                id="src-name"
                autocomplete="off"
                spellcheck="false"
                [invalid]="!!errors().name"
                [value]="name()"
                [disabled]="store.adding()"
                (input)="name.set(text($event))"
              />
            </z-field>
            <z-field
              [label]="i18n.t('sources.field.source')"
              for="src-source"
              [hint]="i18n.t('sources.field.sourceHint')"
              [error]="errors().source ?? ''"
            >
              <input
                zInput
                mono
                id="src-source"
                autocomplete="off"
                spellcheck="false"
                placeholder="git+https://example.com/team/plugins.git"
                [invalid]="!!errors().source"
                [value]="source()"
                [disabled]="store.adding()"
                (input)="source.set(text($event))"
              />
            </z-field>
            <z-field
              [label]="i18n.t('sources.field.ref')"
              for="src-ref"
              [hint]="i18n.t('sources.field.refHint')"
              [error]="errors().ref ?? ''"
            >
              <input
                zInput
                mono
                id="src-ref"
                autocomplete="off"
                spellcheck="false"
                [invalid]="!!errors().ref"
                [value]="ref()"
                [disabled]="store.adding()"
                (input)="ref.set(text($event))"
              />
            </z-field>
            <z-field
              [label]="i18n.t('sources.field.path')"
              for="src-path"
              [hint]="i18n.t('sources.field.pathHint')"
              [error]="errors().path ?? ''"
            >
              <input
                zInput
                mono
                id="src-path"
                autocomplete="off"
                spellcheck="false"
                [invalid]="!!errors().path"
                [value]="path()"
                [disabled]="store.adding()"
                (input)="path.set(text($event))"
              />
            </z-field>
            <div>
              <z-option-group
                [legend]="i18n.t('sources.field.trust')"
                [options]="trustOptions()"
                [(value)]="trust"
                [disabled]="store.adding()"
              />
              @if (errors().trust) {
                <span class="z-field__error" role="alert">{{ errors().trust }}</span>
              }
            </div>
            @if (generalError()) {
              <z-alert status="danger" icon="error">{{ generalError() }}</z-alert>
            }
            @if (store.adding()) {
              <p class="z-muted" role="status">{{ i18n.t("sources.adding") }}</p>
            }
            <div class="z-cluster">
              <button zBtn="primary" type="submit" [loading]="store.adding()">
                {{ i18n.t("sources.submit") }}
              </button>
              <button zBtn="ghost" type="button" [disabled]="store.adding()" (click)="closeForm()">
                {{ i18n.t("common.cancel") }}
              </button>
            </div>
          </form>
        </z-panel>
      }

      @if (error()) {
        <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
          {{ error() }}
          <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="reload()">
            {{ i18n.t("common.retry") }}
          </button>
        </z-alert>
      } @else if (store.state() === "idle" || store.state() === "loading") {
        <z-panel flush [busy]="true">
          <div class="z-stack loading" role="status" [attr.aria-label]="i18n.t('common.loading')">
            <z-skeleton width="60%" />
            <z-skeleton width="80%" />
            <z-skeleton width="50%" />
          </div>
        </z-panel>
      } @else if (views().length === 0) {
        <z-panel flush>
          <z-empty-state [title]="i18n.t('sources.empty')">
            {{ i18n.t("sources.emptyHint") }}
            @if (!formOpen()) {
              <button zEmptyAction zBtn="secondary" type="button" (click)="toggleForm()">
                {{ i18n.t("sources.add") }}
              </button>
            }
          </z-empty-state>
        </z-panel>
      } @else {
        @for (view of views(); track view.repository.name) {
          <z-panel
            headingLevel="2"
            titleMono
            [title]="view.repository.name"
            [busy]="view.busy"
            [attr.data-source]="view.repository.name"
          >
            <div zPanelActions class="z-cluster">
              @if (view.repository.pending) {
                <button
                  zBtn="primary"
                  size="sm"
                  type="button"
                  [disabled]="view.busy"
                  (click)="review(view.repository)"
                >
                  <z-icon name="fact_check" />{{ i18n.t("sources.review") }}
                </button>
              }
              @if (view.syncable) {
                <button
                  zBtn="secondary"
                  size="sm"
                  type="button"
                  [loading]="view.busy"
                  (click)="sync(view.repository)"
                >
                  <z-icon name="sync" />{{ i18n.t("sources.sync") }}
                </button>
              }
              @if (view.removable) {
                <button
                  zBtn="ghost"
                  size="sm"
                  type="button"
                  [disabled]="view.busy"
                  (click)="remove(view.repository)"
                >
                  <z-icon name="delete" />{{ i18n.t("sources.remove") }}
                </button>
              }
            </div>
            <div class="z-stack">
              <div class="z-cluster">
                <z-badge [status]="view.badge" dot>{{ view.state }}</z-badge>
                @if (view.repository.state === "unsupported") {
                  <span class="z-muted">
                    {{ i18n.t("sources.unsupported", { kind: view.repository.kind }) }}
                  </span>
                }
              </div>
              @if (view.repository.lastError) {
                <z-alert status="danger" icon="error" [title]="i18n.t('sources.errorTitle')">
                  <span class="z-mono error-text">{{ view.repository.lastError }}</span>
                </z-alert>
              }
              <z-spec-list [items]="view.facts" />
              @if (!view.removable) {
                <p class="z-subtle caption">
                  <z-icon name="lock" size="sm" /> {{ i18n.t("sources.removeLocked") }}
                </p>
              }
            </div>
          </z-panel>
        }
      }
    </div>
  `,
  styles: `
    .loading {
      padding: var(--space-4);
    }
    .error-text {
      overflow-wrap: anywhere;
    }
  `,
})
export class SourcesPage {
  protected readonly i18n = inject(I18n);
  protected readonly store = inject(RepositoriesStore);
  private readonly dialog = inject(ZDialog);
  private readonly toast = inject(ZToast);

  protected readonly formOpen = signal(false);
  protected readonly name = signal("");
  protected readonly source = signal("");
  protected readonly ref = signal("");
  protected readonly path = signal("");
  protected readonly trust = signal<Trust>("prompt");
  private readonly submitted = signal(false);
  private readonly serverErrors = signal<FieldErrors>({});
  protected readonly generalError = signal("");

  protected readonly trustOptions = computed<ZOption<Trust>[]>(() => [
    {
      value: "prompt",
      title: this.i18n.t("sources.trust.prompt"),
      description: this.i18n.t("sources.trust.promptHelp"),
    },
    {
      value: "pinned",
      title: this.i18n.t("sources.trust.pinned"),
      description: this.i18n.t("sources.trust.pinnedHelp"),
    },
  ]);

  /** Client checks once a submit was tried, then the answers of the core for what it refused. */
  protected readonly errors = computed<FieldErrors>(() => {
    const client = this.submitted() ? validateSource(this.i18n, this.values()) : {};
    return { ...this.serverErrors(), ...client };
  });

  protected readonly error = computed(() => {
    const problem = this.store.error();
    return this.store.state() === "error" && problem ? problemText(this.i18n, problem) : "";
  });

  protected readonly views = computed<RepositoryView[]>(() =>
    this.store.repositories().map((repository) => this.view(repository)),
  );

  constructor() {
    void this.store.ensureLoaded();
  }

  protected reload(): void {
    void this.store.reload();
  }

  protected text(event: Event): string {
    return (event.target as HTMLInputElement).value;
  }

  protected toggleForm(): void {
    if (this.formOpen()) {
      this.closeForm();
    } else {
      this.formOpen.set(true);
    }
  }

  protected closeForm(): void {
    this.formOpen.set(false);
    this.submitted.set(false);
    this.serverErrors.set({});
    this.generalError.set("");
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.store.adding()) {
      return;
    }
    this.submitted.set(true);
    this.serverErrors.set({});
    this.generalError.set("");
    if (Object.keys(validateSource(this.i18n, this.values())).length > 0) {
      return;
    }
    const values = this.values();
    const body: RepositoryCreate = {
      name: values.name,
      source: values.source,
      trust: values.trust,
      ...(values.ref ? { ref: values.ref } : {}),
      ...(values.path ? { path: values.path } : {}),
    };
    try {
      const created = await this.store.add(body);
      this.reset();
      this.closeForm();
      if (created.state === "error") {
        this.toast.warning(this.i18n.t("sources.addedFailed", { name: created.name }));
      } else {
        this.toast.success(this.i18n.t("sources.added", { name: created.name }));
      }
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.showProblem(error);
      } else {
        throw error;
      }
    }
  }

  protected async sync(repository: Repository): Promise<void> {
    const name = repository.name;
    try {
      const result = await this.store.sync(name);
      if (result.state === "error") {
        this.toast.warning(this.i18n.t("sources.syncedFailed", { name }));
      } else if (result.state === "pending") {
        this.toast.info(this.i18n.t("sources.syncedPending", { name }));
      } else {
        this.toast.success(this.i18n.t("sources.synced", { name }));
      }
    } catch (error) {
      this.fail(error);
    }
  }

  protected review(repository: Repository): void {
    const revision = repository.pending?.revision;
    if (!revision) {
      return;
    }
    this.dialog.open<RepositoryReviewResult, RepositoryReviewData>(RepositoryReview, {
      data: { name: repository.name, revision },
    });
  }

  protected async remove(repository: Repository): Promise<void> {
    const name = repository.name;
    const confirmed = await firstValueFrom(
      this.dialog.confirm({
        title: this.i18n.t("sources.removeTitle", { name }),
        body: this.i18n.t("sources.removeBody"),
        confirmLabel: this.i18n.t("sources.remove"),
        cancelLabel: this.i18n.t("common.cancel"),
        danger: true,
      }),
    );
    if (!confirmed) {
      return;
    }
    try {
      await this.store.remove(name);
      this.toast.success(this.i18n.t("sources.removed", { name }));
    } catch (error) {
      this.fail(error);
    }
  }

  private values(): { name: string; source: string; ref: string; path: string; trust: Trust } {
    return {
      name: this.name().trim(),
      source: this.source().trim(),
      ref: this.ref().trim(),
      path: this.path().trim(),
      trust: this.trust(),
    };
  }

  private reset(): void {
    this.name.set("");
    this.source.set("");
    this.ref.set("");
    this.path.set("");
    this.trust.set("prompt");
  }

  private fail(error: unknown): void {
    if (error instanceof ApiProblem) {
      this.toast.error(problemText(this.i18n, error));
    } else {
      throw error;
    }
  }

  /** Field errors by pointer (`/name`); anything else goes to the general alert. */
  private showProblem(problem: ApiProblem): void {
    const fields: FieldErrors = {};
    let rest = problem.errors.length === 0;
    for (const error of problem.errors) {
      const match = /^\/(name|source|ref|path|trust)$/.exec(error.pointer);
      if (match?.[1]) {
        fields[match[1] as Field] = fieldErrorText(this.i18n, error);
      } else {
        rest = true;
      }
    }
    this.serverErrors.set(fields);
    if (rest) {
      this.generalError.set(problemText(this.i18n, problem));
    }
  }

  private view(repository: Repository): RepositoryView {
    const t = (key: string, params?: Record<string, string | number>): string =>
      this.i18n.t(key, params);
    const facts: ZSpecItem[] = [];
    if (repository.source) {
      facts.push({ term: t("sources.label.address"), value: repository.source, mono: true });
    }
    if (repository.ref) {
      facts.push({ term: t("sources.label.ref"), value: repository.ref, mono: true });
    }
    if (repository.path) {
      facts.push({ term: t("sources.label.path"), value: repository.path, mono: true });
    }
    if (repository.trust) {
      facts.push({
        term: t("sources.label.trust"),
        value: t(`sources.trustValue.${repository.trust}`),
      });
    }
    facts.push({
      term: t("sources.label.active"),
      value: repository.active
        ? t("sources.activeValue", {
            revision: repository.active.revision.slice(0, 12),
            count: repository.active.plugins.length,
          })
        : t("sources.noneActive"),
      mono: repository.active !== undefined,
    });
    facts.push({
      term: t("sources.label.lastSync"),
      value: repository.lastSync
        ? t(repository.lastSync.changed ? "sources.synced.changed" : "sources.synced.unchanged", {
            date: this.i18n.date(repository.lastSync.at, "short"),
          })
        : t("sources.never"),
    });
    facts.push({ term: t("sources.label.origin"), value: layerName(this.i18n, repository.origin) });
    const live = repository.state !== "unsupported" && repository.state !== "disabled";
    return {
      repository,
      badge: STATE_BADGE[repository.state],
      state: t(`sources.state.${repository.state}`),
      facts,
      busy: this.store.busy().has(repository.name),
      removable: repository.origin === "user" && !repository.locked,
      syncable: live,
    };
  }
}
