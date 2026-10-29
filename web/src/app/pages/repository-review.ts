import { DIALOG_DATA, DialogRef } from "@angular/cdk/dialog";
import { ChangeDetectionStrategy, Component, computed, inject, signal } from "@angular/core";
import { ZAlert, ZBadge, ZButton, ZDialogActions, ZDialogLayout, ZIcon, ZToast } from "zenit-ui";
import { ApiProblem } from "../api/problem";
import type { RepositoryPermission, RepositoryPlugin } from "../api/types";
import { problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { RepositoriesStore } from "../stores/repositories.store";

export interface RepositoryReviewData {
  /** Name of the repository. */
  name: string;
  /** The pending revision shown when the dialog opened; only this one can be approved. */
  revision: string;
}

export type RepositoryReviewResult = "approved" | "discarded";

type Change = "added" | "changed" | "removed";

interface PermissionView {
  key: string;
  kind: RepositoryPermission["kind"];
  text: string;
  isNew: boolean;
}

interface PluginView {
  id: string;
  title: string;
  version: string;
  description: string;
  permissions: PermissionView[];
  requires: string;
  error: string;
}

interface GroupView {
  change: Change;
  title: string;
  plugins: PluginView[];
}

const GROUPS: readonly Change[] = ["added", "changed", "removed"];

/**
 * Review of a pending revision: the plugins grouped by change with the permissions they
 * request. New permissions are marked, sudo is called out. Approving sends the revision that
 * was shown; when another revision arrives meanwhile, approving is blocked.
 */
@Component({
  selector: "dbx-repository-review",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ZAlert, ZBadge, ZButton, ZDialogActions, ZDialogLayout, ZIcon],
  template: `
    <z-dialog [title]="i18n.t('review.title', { name: data.name })">
      <div class="z-stack">
        <p class="z-muted">
          {{ i18n.t("review.sub", { revision: shortRevision(), date: fetched() }) }}
        </p>
        <p>{{ i18n.t("review.intro") }}</p>
        @if (stale()) {
          <z-alert status="warning" icon="warning">{{ i18n.t("review.stale") }}</z-alert>
        }
        @if (sudoPlugins().length > 0) {
          <z-alert status="danger" icon="lock" [title]="i18n.t('review.sudoTitle')">
            {{ i18n.t("review.sudoBody", { plugins: sudoPlugins().join(", ") }) }}
          </z-alert>
        }
        @for (group of groups(); track group.change) {
          <section class="group" [attr.aria-label]="group.title">
            <h3 class="title-sm">{{ group.title }} ({{ group.plugins.length }})</h3>
            @for (plugin of group.plugins; track plugin.id) {
              <article class="plugin" [attr.aria-label]="plugin.title">
                <div class="z-cluster">
                  <strong>{{ plugin.title }}</strong>
                  <span class="z-mono z-muted">{{ plugin.version }}</span>
                </div>
                @if (plugin.description) {
                  <span class="z-muted">{{ plugin.description }}</span>
                }
                @if (plugin.error) {
                  <span class="z-field__error">
                    {{ i18n.t("review.pluginError", { error: plugin.error }) }}
                  </span>
                }
                @if (group.change === "removed") {
                  <span class="z-subtle caption">{{ i18n.t("review.removedHint") }}</span>
                } @else {
                  @if (plugin.permissions.length === 0) {
                    <span class="z-subtle caption">{{ i18n.t("review.noPermissions") }}</span>
                  } @else {
                    <ul class="perms" [attr.aria-label]="i18n.t('review.permissions')">
                      @for (perm of plugin.permissions; track perm.key) {
                        <li [class.sudo]="perm.kind === 'sudo'">
                          @if (perm.kind === "sudo") {
                            <z-icon name="lock" size="sm" />
                          }
                          <span [class.z-mono]="perm.kind !== 'sudo'">{{ perm.text }}</span>
                          @if (perm.isNew) {
                            <z-badge [status]="perm.kind === 'sudo' ? 'danger' : 'warning'">
                              {{ i18n.t("review.new") }}
                            </z-badge>
                          }
                        </li>
                      }
                    </ul>
                  }
                  @if (plugin.requires) {
                    <span class="z-subtle caption">{{ plugin.requires }}</span>
                  }
                }
              </article>
            }
          </section>
        }
        @if (unchanged() > 0) {
          <p class="z-muted">{{ i18n.t("review.unchanged", { count: unchanged() }) }}</p>
        }
        @if (groups().length === 0 && unchanged() === 0) {
          <p class="z-muted">{{ i18n.t("review.nothing") }}</p>
        }
        @if (error()) {
          <z-alert status="danger" icon="error" [title]="i18n.t('review.failed')">
            {{ error() }}
          </z-alert>
        }
      </div>
      <ng-container zDialogActions>
        <button zBtn="ghost" type="button" [disabled]="busy()" (click)="ref.close()">
          {{ i18n.t("common.close") }}
        </button>
        <button
          zBtn="secondary"
          type="button"
          [disabled]="busy() || !pending()"
          [loading]="working() === 'discard'"
          (click)="discard()"
        >
          {{ i18n.t("review.discard") }}
        </button>
        <button
          zBtn="primary"
          type="button"
          [disabled]="busy() || stale()"
          [loading]="working() === 'approve'"
          (click)="approve()"
        >
          {{ i18n.t("review.approve") }}
        </button>
      </ng-container>
    </z-dialog>
  `,
  styles: `
    .group {
      display: grid;
      gap: var(--space-3);
    }
    .plugin {
      display: grid;
      gap: var(--space-2);
      padding-block-end: var(--space-3);
      border-block-end: thin solid var(--border);
    }
    .plugin:last-child {
      border-block-end: 0;
      padding-block-end: 0;
    }
    .perms {
      display: grid;
      gap: var(--space-1);
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .perms li {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-2);
    }
    .perms li.sudo {
      color: var(--danger);
      font-weight: 600;
    }
  `,
})
export class RepositoryReview {
  protected readonly data = inject<RepositoryReviewData>(DIALOG_DATA);
  protected readonly ref = inject<DialogRef<RepositoryReviewResult>>(DialogRef);
  protected readonly i18n = inject(I18n);
  private readonly store = inject(RepositoriesStore);
  private readonly toast = inject(ZToast);

  protected readonly working = signal<"approve" | "discard" | null>(null);
  protected readonly busy = computed(
    () => this.working() !== null || this.store.busy().has(this.data.name),
  );
  protected readonly error = signal("");

  private readonly repository = computed(() => this.store.repository(this.data.name));
  protected readonly pending = computed(() => this.repository()?.pending);
  /** True when the pending revision is not the one this dialog opened with. */
  protected readonly stale = computed(() => this.pending()?.revision !== this.data.revision);

  protected readonly shortRevision = computed(() => this.data.revision.slice(0, 12));
  protected readonly fetched = computed(() => {
    const at = this.pending()?.fetchedAt;
    return at ? this.i18n.date(at, "short") : "";
  });

  private readonly plugins = computed(() => this.pending()?.plugins ?? []);

  protected readonly groups = computed<GroupView[]>(() =>
    GROUPS.map((change) => ({
      change,
      title: this.i18n.t(`review.group.${change}`),
      plugins: this.plugins()
        .filter((p) => p.change === change)
        .map((p) => this.view(p)),
    })).filter((group) => group.plugins.length > 0),
  );

  protected readonly unchanged = computed(
    () => this.plugins().filter((p) => p.change === "unchanged").length,
  );

  /** Plugins of the revision that ask for sudo and will be loaded. */
  protected readonly sudoPlugins = computed(() =>
    this.plugins()
      .filter((p) => p.change !== "removed" && p.permissions.some((perm) => perm.kind === "sudo"))
      .map((p) => p.name ?? p.id),
  );

  protected async approve(): Promise<void> {
    await this.run("approve", async () => {
      await this.store.approve(this.data.name, this.data.revision);
      this.toast.success(this.i18n.t("review.approved", { name: this.data.name }));
      this.ref.close("approved");
    });
  }

  protected async discard(): Promise<void> {
    await this.run("discard", async () => {
      await this.store.discard(this.data.name);
      this.toast.success(this.i18n.t("review.discarded", { name: this.data.name }));
      this.ref.close("discarded");
    });
  }

  private async run(kind: "approve" | "discard", call: () => Promise<void>): Promise<void> {
    this.error.set("");
    this.working.set(kind);
    try {
      await call();
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.error.set(problemText(this.i18n, error));
      } else {
        throw error;
      }
    } finally {
      this.working.set(null);
    }
  }

  private view(plugin: RepositoryPlugin): PluginView {
    return {
      id: plugin.id,
      title: plugin.name ?? plugin.id,
      version:
        plugin.change === "changed" && plugin.previousVersion
          ? this.i18n.t("review.versionChange", {
              from: plugin.previousVersion,
              to: plugin.version ?? "",
            })
          : (plugin.version ?? ""),
      description: plugin.description ?? "",
      permissions: plugin.permissions.map((perm) => ({
        key: `${perm.kind}:${perm.value}`,
        kind: perm.kind,
        text: this.i18n.t(`review.perm.${perm.kind}`, { value: perm.value }),
        isNew: perm.new,
      })),
      requires:
        plugin.requiresCommands.length > 0
          ? this.i18n.t("review.requires", { commands: plugin.requiresCommands.join(", ") })
          : "",
      error: plugin.error ?? "",
    };
  }
}
