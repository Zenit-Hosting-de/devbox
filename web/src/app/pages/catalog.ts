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
  ZInputGroup,
  ZPageHeader,
  ZPanel,
  ZSkeleton,
  ZToast,
} from "zenit-ui";
import { ApiClient } from "../api/api-client";
import { ApiProblem, toApiProblem } from "../api/problem";
import type { CatalogHit, CatalogInstall, CatalogPermission } from "../api/types";
import { problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { PluginsStore } from "../stores/plugins.store";
import { PluginTabs } from "./plugin-tabs";

type State = "loading" | "ready" | "error";

/** Search the catalogs, install and update plugins after confirming their permissions, remove them. */
@Component({
  selector: "dbx-catalog-page",
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
    ZInputGroup,
    ZPageHeader,
    ZPanel,
    ZSkeleton,
  ],
  template: `
    <z-page-header [title]="i18n.t('nav.plugins')" [sub]="i18n.t('catalog.sub')" />
    <dbx-plugin-tabs active="catalog" />

    <div class="z-stack">
      <form class="search" role="search" (submit)="search($event)">
        <z-field [label]="i18n.t('catalog.search')" for="catalog-search">
          <z-input-group icon="search">
            <input
              zInput
              id="catalog-search"
              type="search"
              autocomplete="off"
              [value]="query()"
              [placeholder]="i18n.t('catalog.searchPlaceholder')"
              (input)="query.set(text($event))"
            />
          </z-input-group>
        </z-field>
      </form>

      @for (problem of problems(); track problem.name) {
        <z-alert
          status="warning"
          icon="warning"
          [title]="i18n.t('catalog.problem', { name: problem.name })"
        >
          <span class="z-mono error-text">{{ problem.text }}</span>
        </z-alert>
      }

      @if (state() === "error") {
        <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
          {{ error() }}
          <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="load()">
            {{ i18n.t("common.retry") }}
          </button>
        </z-alert>
      } @else if (state() === "loading") {
        <z-panel>
          <div class="z-stack" role="status" [attr.aria-label]="i18n.t('common.loading')">
            <z-skeleton width="60%" />
            <z-skeleton width="40%" />
          </div>
        </z-panel>
      } @else {
        @for (hit of hits(); track hit.catalog + "/" + hit.id) {
          <z-panel
            headingLevel="2"
            [title]="hit.name"
            [busy]="busy() === hit.id"
            [attr.data-hit]="hit.id"
          >
            <div class="z-stack">
              <div class="z-cluster">
                <span class="z-mono">{{ hit.id }}</span>
                <z-badge status="neutral">{{ hit.version }}</z-badge>
                <span class="z-muted">{{ i18n.t("catalog.from", { name: hit.catalog }) }}</span>
                @if (hit.installed) {
                  <z-badge status="success" dot>
                    {{ i18n.t("catalog.installed", { version: hit.installed }) }}
                  </z-badge>
                }
              </div>
              @if (hit.description) {
                <p class="z-muted desc">{{ hit.description }}</p>
              }
              <div class="z-cluster">
                @if (!hit.installed) {
                  <button
                    zBtn="primary"
                    size="sm"
                    type="button"
                    [disabled]="busy() !== null"
                    (click)="install(hit)"
                  >
                    <z-icon name="download" />{{ i18n.t("catalog.install") }}
                  </button>
                } @else {
                  @if (hit.installed !== hit.version) {
                    <button
                      zBtn="primary"
                      size="sm"
                      type="button"
                      [disabled]="busy() !== null"
                      (click)="install(hit)"
                    >
                      <z-icon name="upgrade" />{{
                        i18n.t("catalog.update", { version: hit.version })
                      }}
                    </button>
                  }
                  <button
                    zBtn="ghost"
                    size="sm"
                    type="button"
                    [disabled]="busy() !== null"
                    (click)="remove(hit)"
                  >
                    <z-icon name="delete" />{{ i18n.t("catalog.remove") }}
                  </button>
                }
              </div>
            </div>
          </z-panel>
        } @empty {
          <z-panel>
            <z-empty-state [title]="i18n.t('catalog.empty')">
              {{ i18n.t("catalog.emptyHint") }}
            </z-empty-state>
          </z-panel>
        }
      }
    </div>
  `,
  styles: `
    .desc {
      margin: 0;
    }
    .error-text {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
  `,
})
export class CatalogPage {
  protected readonly i18n = inject(I18n);
  private readonly api = inject(ApiClient);
  private readonly plugins = inject(PluginsStore);
  private readonly dialog = inject(ZDialog);
  private readonly toast = inject(ZToast);

  protected readonly query = signal("");
  protected readonly state = signal<State>("loading");
  protected readonly hits = signal<readonly CatalogHit[]>([]);
  private readonly problemMap = signal<Record<string, string>>({});
  protected readonly error = signal("");
  /** ID of the plugin with a call in flight. */
  protected readonly busy = signal<string | null>(null);
  protected readonly problems = computed(() =>
    Object.entries(this.problemMap()).map(([name, text]) => ({ name, text })),
  );

  constructor() {
    void this.load();
  }

  protected text(event: Event): string {
    return (event.target as HTMLInputElement).value;
  }

  protected search(event: Event): void {
    event.preventDefault();
    void this.load();
  }

  async load(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    try {
      const result = await this.api.searchCatalog(this.query().trim());
      this.hits.set(result.plugins);
      this.problemMap.set(result.problems);
      this.state.set("ready");
    } catch (error) {
      this.error.set(problemText(this.i18n, toApiProblem(error)));
      this.state.set("error");
    }
  }

  /** Checks the package first; asks for its permissions when the core says it must. */
  protected async install(hit: CatalogHit): Promise<void> {
    this.busy.set(hit.id);
    try {
      let result = await this.api.installFromCatalog({
        id: hit.id,
        catalog: hit.catalog,
        confirm: false,
      });
      if (!result.installed) {
        if (!(await this.confirm(result))) {
          return;
        }
        result = await this.api.installFromCatalog({
          id: hit.id,
          catalog: hit.catalog,
          version: result.version,
          confirm: true,
        });
      }
      this.toast.success(
        this.i18n.t(result.previousVersion ? "catalog.updated" : "catalog.done", {
          id: result.id,
          version: result.version,
        }),
      );
      void this.plugins.reload();
      await this.load();
    } catch (error) {
      this.fail(error);
    } finally {
      this.busy.set(null);
    }
  }

  private confirm(plan: CatalogInstall): Promise<boolean> {
    const update = !!plan.previousVersion;
    const list = update ? plan.newPermissions : plan.permissions;
    const lines = list.length > 0 ? list.map((p) => this.permission(p)).join("\n") : "";
    return firstValueFrom(
      this.dialog.confirm({
        title: this.i18n.t(update ? "catalog.confirmUpdate" : "catalog.confirmInstall", {
          id: plan.id,
          version: plan.version,
        }),
        body:
          (lines
            ? this.i18n.t(update ? "catalog.newPermissions" : "catalog.permissions")
            : this.i18n.t("catalog.noPermissions")) +
          (lines ? "\n" + lines : "") +
          "\n" +
          this.i18n.t("catalog.signedBy", { identity: plan.signedBy ?? "" }),
        confirmLabel: this.i18n.t(update ? "catalog.confirmUpdateButton" : "catalog.install"),
        cancelLabel: this.i18n.t("common.cancel"),
      }),
    ).then((ok) => ok === true);
  }

  private permission(p: CatalogPermission): string {
    return `${this.i18n.t(`catalog.perm.${p.kind}`)}: ${p.value}`;
  }

  protected async remove(hit: CatalogHit): Promise<void> {
    const confirmed = await firstValueFrom(
      this.dialog.confirm({
        title: this.i18n.t("catalog.removeTitle", { id: hit.id }),
        body: this.i18n.t("catalog.removeBody"),
        confirmLabel: this.i18n.t("catalog.remove"),
        cancelLabel: this.i18n.t("common.cancel"),
        danger: true,
      }),
    );
    if (!confirmed) {
      return;
    }
    this.busy.set(hit.id);
    try {
      await this.api.deletePlugin(hit.id);
      this.toast.success(this.i18n.t("catalog.removed", { id: hit.id }));
      void this.plugins.reload();
      await this.load();
    } catch (error) {
      this.fail(error);
    } finally {
      this.busy.set(null);
    }
  }

  private fail(error: unknown): void {
    const problem = error instanceof ApiProblem ? error : toApiProblem(error);
    this.toast.error(problemText(this.i18n, problem));
  }
}
