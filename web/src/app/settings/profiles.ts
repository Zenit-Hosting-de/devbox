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
  ZSkeleton,
  ZSpecList,
  ZToast,
  type ZSpecItem,
} from "zenit-ui";
import { ApiProblem } from "../api/problem";
import type { Profile, ProfileCreate } from "../api/types";
import { fieldErrorText, layerName, problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { ProfilesStore } from "../stores/profiles.store";

const SOURCE_PATTERN = /^(file|https|git\+https|git\+ssh):\/\/\S+$/;

type Field = "source" | "ref" | "path" | "issuer" | "identity";
export type ProfileErrors = Partial<Record<Field, string>>;

/** Client-side checks of the form; the core validates again. Values are trimmed. */
export function validateProfile(
  i18n: I18n,
  values: { source: string; ref: string; issuer: string; identity: string },
): ProfileErrors {
  const errors: ProfileErrors = {};
  if (values.source === "") {
    errors.source = i18n.t("form.error.required");
  } else if (!SOURCE_PATTERN.test(values.source)) {
    errors.source = i18n.t("profiles.error.source");
  } else if (values.ref !== "" && !values.source.startsWith("git+")) {
    errors.ref = i18n.t("profiles.error.refGit");
  }
  if (values.issuer !== "" && values.identity === "") {
    errors.identity = i18n.t("profiles.error.trustBoth");
  } else if (values.identity !== "" && values.issuer === "") {
    errors.issuer = i18n.t("profiles.error.trustBoth");
  }
  return errors;
}

interface ProfileView {
  profile: Profile;
  key: string;
  title: string;
  facts: ZSpecItem[];
  removable: boolean;
  busy: boolean;
}

/** Profiles of teams: list with origin, lock and signature, add, fetch again and remove. */
@Component({
  selector: "dbx-profiles",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZField,
    ZIcon,
    ZInput,
    ZSkeleton,
    ZSpecList,
  ],
  template: `
    <div class="z-stack">
      <p class="z-muted">{{ i18n.t("profiles.intro") }}</p>
      <div class="z-cluster">
        <button
          zBtn="secondary"
          size="sm"
          type="button"
          [attr.aria-expanded]="formOpen()"
          aria-controls="profile-form"
          (click)="toggleForm()"
        >
          <z-icon name="add" />{{ i18n.t("profiles.add") }}
        </button>
        @if (hasRemote()) {
          <button
            zBtn="ghost"
            size="sm"
            type="button"
            [loading]="store.updating()"
            (click)="update()"
          >
            <z-icon name="sync" />{{ i18n.t("profiles.update") }}
          </button>
        }
      </div>

      @if (formOpen()) {
        <form id="profile-form" class="z-stack" novalidate (submit)="submit($event)">
          <z-field
            [label]="i18n.t('profiles.field.source')"
            for="profile-source"
            [hint]="i18n.t('profiles.field.sourceHint')"
            [error]="errors().source ?? ''"
          >
            <input
              zInput
              mono
              id="profile-source"
              autocomplete="off"
              spellcheck="false"
              placeholder="git+https://git.example.com/platform/devbox-profile.git"
              [invalid]="!!errors().source"
              [value]="source()"
              [disabled]="store.saving()"
              (input)="source.set(text($event))"
            />
          </z-field>
          <z-field
            [label]="i18n.t('profiles.field.ref')"
            for="profile-ref"
            [hint]="i18n.t('profiles.field.refHint')"
            [error]="errors().ref ?? ''"
          >
            <input
              zInput
              mono
              id="profile-ref"
              autocomplete="off"
              spellcheck="false"
              [invalid]="!!errors().ref"
              [value]="ref()"
              [disabled]="store.saving()"
              (input)="ref.set(text($event))"
            />
          </z-field>
          <z-field
            [label]="i18n.t('profiles.field.path')"
            for="profile-path"
            [hint]="i18n.t('profiles.field.pathHint')"
            [error]="errors().path ?? ''"
          >
            <input
              zInput
              mono
              id="profile-path"
              autocomplete="off"
              spellcheck="false"
              placeholder="profile.json"
              [invalid]="!!errors().path"
              [value]="path()"
              [disabled]="store.saving()"
              (input)="path.set(text($event))"
            />
          </z-field>
          <z-field
            [label]="i18n.t('profiles.field.issuer')"
            for="profile-issuer"
            [hint]="i18n.t('profiles.field.issuerHint')"
            [error]="errors().issuer ?? ''"
          >
            <input
              zInput
              mono
              id="profile-issuer"
              autocomplete="off"
              spellcheck="false"
              placeholder="https://issuer.example.com"
              [invalid]="!!errors().issuer"
              [value]="issuer()"
              [disabled]="store.saving()"
              (input)="issuer.set(text($event))"
            />
          </z-field>
          <z-field
            [label]="i18n.t('profiles.field.identity')"
            for="profile-identity"
            [error]="errors().identity ?? ''"
          >
            <input
              zInput
              mono
              id="profile-identity"
              autocomplete="off"
              spellcheck="false"
              [invalid]="!!errors().identity"
              [value]="identity()"
              [disabled]="store.saving()"
              (input)="identity.set(text($event))"
            />
          </z-field>
          @if (generalError()) {
            <z-alert status="danger" icon="error">
              <span class="z-mono error-text">{{ generalError() }}</span>
            </z-alert>
          }
          <div class="z-cluster">
            <button zBtn="primary" type="submit" [loading]="store.saving()">
              {{ i18n.t("profiles.submit") }}
            </button>
            <button zBtn="ghost" type="button" [disabled]="store.saving()" (click)="closeForm()">
              {{ i18n.t("common.cancel") }}
            </button>
          </div>
        </form>
      }

      @if (error()) {
        <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
          {{ error() }}
          <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="reload()">
            {{ i18n.t("common.retry") }}
          </button>
        </z-alert>
      } @else if (store.state() === "idle" || store.state() === "loading") {
        <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
          <z-skeleton width="50%" />
          <z-skeleton width="70%" />
        </div>
      } @else {
        @for (view of views(); track view.key) {
          <section class="z-stack profile" [attr.data-profile]="view.key">
            <div class="z-cluster head">
              <h3 class="title">{{ view.title }}</h3>
              <z-badge status="neutral">{{ origin(view.profile) }}</z-badge>
              @if (view.profile.trust) {
                <z-badge status="success" dot>{{ i18n.t("profiles.signed") }}</z-badge>
              }
              @if (view.removable) {
                <button
                  zBtn="ghost"
                  size="sm"
                  type="button"
                  class="remove"
                  [disabled]="view.busy"
                  (click)="remove(view.profile)"
                >
                  <z-icon name="delete" />{{ i18n.t("profiles.remove") }}
                </button>
              }
            </div>
            @if (view.profile.problem) {
              <z-alert status="danger" icon="error" [title]="i18n.t('profiles.problem')">
                <span class="z-mono error-text">{{ view.profile.problem }}</span>
              </z-alert>
            }
            <z-spec-list [items]="view.facts" />
          </section>
        } @empty {
          <z-empty-state [title]="i18n.t('profiles.empty')">
            {{ i18n.t("profiles.emptyHint") }}
          </z-empty-state>
        }
      }
    </div>
  `,
  styles: `
    .head {
      align-items: center;
    }
    .title {
      margin: 0;
      font-size: var(--z-text-md, 1rem);
    }
    .remove {
      margin-inline-start: auto;
    }
    .error-text {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
  `,
})
export class Profiles {
  protected readonly i18n = inject(I18n);
  protected readonly store = inject(ProfilesStore);
  private readonly dialog = inject(ZDialog);
  private readonly toast = inject(ZToast);

  protected readonly formOpen = signal(false);
  protected readonly source = signal("");
  protected readonly ref = signal("");
  protected readonly path = signal("");
  protected readonly issuer = signal("");
  protected readonly identity = signal("");
  private readonly submitted = signal(false);
  private readonly serverErrors = signal<ProfileErrors>({});
  protected readonly generalError = signal("");

  protected readonly errors = computed<ProfileErrors>(() => {
    const client = this.submitted() ? validateProfile(this.i18n, this.values()) : {};
    return { ...this.serverErrors(), ...client };
  });
  protected readonly error = computed(() => {
    const problem = this.store.error();
    return this.store.state() === "error" && problem ? problemText(this.i18n, problem) : "";
  });
  protected readonly hasRemote = computed(() =>
    this.store.profiles().some((p) => p.kind === "https" || p.kind === "git"),
  );
  protected readonly views = computed<ProfileView[]>(() =>
    this.store.profiles().map((profile) => this.view(profile)),
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

  protected origin(profile: Profile): string {
    return layerName(this.i18n, profile.origin);
  }

  protected toggleForm(): void {
    if (this.formOpen()) {
      this.closeForm();
    } else {
      this.reset();
      this.formOpen.set(true);
    }
  }

  protected closeForm(): void {
    this.reset();
    this.formOpen.set(false);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.store.saving()) {
      return;
    }
    this.submitted.set(true);
    this.serverErrors.set({});
    this.generalError.set("");
    const values = this.values();
    if (Object.keys(validateProfile(this.i18n, values)).length > 0) {
      return;
    }
    const body: ProfileCreate = {
      source: values.source,
      ...(values.ref ? { ref: values.ref } : {}),
      ...(values.path ? { path: values.path } : {}),
      ...(values.issuer ? { trust: { issuer: values.issuer, identity: values.identity } } : {}),
    };
    try {
      const created = await this.store.add(body);
      this.closeForm();
      this.toast.success(this.i18n.t("profiles.added", { name: created.name ?? created.id }));
    } catch (error) {
      if (!(error instanceof ApiProblem)) {
        throw error;
      }
      const fields: ProfileErrors = {};
      for (const e of error.errors) {
        const field = e.pointer.replace(/^\//, "") as Field;
        if (["source", "ref", "path"].includes(field)) {
          fields[field] = fieldErrorText(this.i18n, e);
        }
      }
      this.serverErrors.set(fields);
      if (Object.keys(fields).length === 0) {
        this.generalError.set(problemText(this.i18n, error));
      }
    }
  }

  protected async update(): Promise<void> {
    try {
      const updates = await this.store.update();
      const failed = updates.filter((u) => u.error);
      const changed = updates.filter((u) => u.changed).length;
      if (failed.length > 0) {
        this.toast.warning(
          this.i18n.t("profiles.updateFailed", {
            names: failed.map((u) => u.id || u.source).join(", "),
          }),
        );
      } else {
        this.toast.success(this.i18n.t("profiles.updated", { count: changed }));
      }
    } catch (error) {
      this.fail(error);
    }
  }

  protected async remove(profile: Profile): Promise<void> {
    const name = profile.name ?? profile.id;
    const confirmed = await firstValueFrom(
      this.dialog.confirm({
        title: this.i18n.t("profiles.removeTitle", { name }),
        body: this.i18n.t("profiles.removeBody"),
        confirmLabel: this.i18n.t("profiles.remove"),
        cancelLabel: this.i18n.t("common.cancel"),
        danger: true,
      }),
    );
    if (!confirmed) {
      return;
    }
    try {
      await this.store.remove(profile.id || profile.source);
      this.toast.success(this.i18n.t("profiles.removed", { name }));
    } catch (error) {
      this.fail(error);
    }
  }

  private view(profile: Profile): ProfileView {
    const t = (key: string, params?: Record<string, string | number>): string =>
      this.i18n.t(key, params);
    const facts: ZSpecItem[] = [
      { term: t("profiles.label.source"), value: profile.source, mono: true },
    ];
    if (profile.ref) {
      facts.push({ term: t("profiles.label.ref"), value: profile.ref, mono: true });
    }
    if (profile.path) {
      facts.push({ term: t("profiles.label.path"), value: profile.path, mono: true });
    }
    if (profile.version) {
      facts.push({ term: t("profiles.label.version"), value: profile.version, mono: true });
    }
    if (profile.trust) {
      facts.push({ term: t("profiles.label.signedBy"), value: profile.trust.identity, mono: true });
    }
    if (profile.fetchedAt) {
      facts.push({
        term: t("profiles.label.fetched"),
        value: this.i18n.date(profile.fetchedAt, "short"),
      });
    }
    if (!profile.problem) {
      facts.push({
        term: t("profiles.label.settings"),
        value: t("profiles.settingsValue", {
          count: profile.settings,
          locked: profile.locked.length,
        }),
      });
    }
    if (profile.locked.length > 0) {
      facts.push({
        term: t("profiles.label.locked"),
        value: profile.locked.join(", "),
        mono: true,
      });
    }
    const key = profile.id || profile.source;
    return {
      profile,
      key,
      title: profile.name ? `${profile.name} (${profile.id})` : key,
      facts,
      removable: profile.origin === "user",
      busy: this.store.busy().has(key),
    };
  }

  private values(): {
    source: string;
    ref: string;
    path: string;
    issuer: string;
    identity: string;
  } {
    return {
      source: this.source().trim(),
      ref: this.ref().trim(),
      path: this.path().trim(),
      issuer: this.issuer().trim(),
      identity: this.identity().trim(),
    };
  }

  private reset(): void {
    this.source.set("");
    this.ref.set("");
    this.path.set("");
    this.issuer.set("");
    this.identity.set("");
    this.submitted.set(false);
    this.serverErrors.set({});
    this.generalError.set("");
  }

  private fail(error: unknown): void {
    if (error instanceof ApiProblem) {
      this.toast.error(problemText(this.i18n, error));
    } else {
      throw error;
    }
  }
}
