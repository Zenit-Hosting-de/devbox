import { ChangeDetectionStrategy, Component, computed, inject } from "@angular/core";
import {
  ZAlert,
  ZAlertAction,
  ZBadge,
  ZButton,
  ZEmptyState,
  ZPageHeader,
  ZPanel,
  ZSkeleton,
  ZTable,
  ZTableContainer,
} from "zenit-ui";
import type { CredentialInstance } from "../api/types";
import { problemText, profileName } from "../core/status";
import { I18n } from "../i18n/i18n";
import { CredentialsStore } from "../stores/credentials.store";

@Component({
  selector: "dbx-credentials-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZPageHeader,
    ZPanel,
    ZSkeleton,
    ZTable,
    ZTableContainer,
  ],
  template: `
    <z-page-header [title]="i18n.t('nav.credentials')" [sub]="i18n.t('credentials.sub')" />
    <div class="z-stack">
      <z-alert status="info" icon="info">{{ i18n.t("credentials.note") }}</z-alert>
      @if (error()) {
        <z-alert status="danger" icon="error" [title]="i18n.t('common.loadFailed')">
          {{ error() }}
          <button zAlertAction zBtn="secondary" size="sm" type="button" (click)="reload()">
            {{ i18n.t("common.retry") }}
          </button>
        </z-alert>
      } @else {
        <z-panel flush [busy]="store.state() === 'loading' || store.state() === 'idle'">
          @if (store.state() === "idle" || store.state() === "loading") {
            <div class="z-stack pad" role="status" [attr.aria-label]="i18n.t('common.loading')">
              <z-skeleton width="70%" />
              <z-skeleton width="50%" />
            </div>
          } @else if (store.instances().length === 0) {
            <z-empty-state [title]="i18n.t('credentials.empty')">
              {{ i18n.t("credentials.emptyHint") }}
            </z-empty-state>
          } @else {
            <z-table-container [ariaLabel]="i18n.t('credentials.tableLabel')">
              <table zTable>
                <thead>
                  <tr>
                    <th>{{ i18n.t("credentials.name") }}</th>
                    <th>{{ i18n.t("credentials.type") }}</th>
                    <th>{{ i18n.t("credentials.origin") }}</th>
                    <th>{{ i18n.t("credentials.secret") }}</th>
                    <th>{{ i18n.t("credentials.verified") }}</th>
                    <th>{{ i18n.t("credentials.consumers") }}</th>
                  </tr>
                </thead>
                <tbody>
                  @for (row of rows(); track row.instance.name) {
                    <tr>
                      <td>
                        <span class="z-mono">{{ row.instance.name }}</span>
                        @if (row.instance.label) {
                          <br /><span class="z-muted">{{ row.instance.label }}</span>
                        }
                      </td>
                      <td class="z-mono">{{ row.instance.type }}</td>
                      <td>{{ row.origin }}</td>
                      <td>
                        @if (row.instance.secret; as secret) {
                          <z-badge status="success" dot>{{
                            i18n.t("credentials.secretSet")
                          }}</z-badge>
                          <br /><span class="z-muted caption">{{ row.expiry }}</span>
                        } @else {
                          <z-badge status="warning" dot>{{
                            i18n.t("credentials.secretMissing")
                          }}</z-badge>
                        }
                      </td>
                      <td>
                        @if (row.instance.verified; as verified) {
                          <z-badge [status]="verified.status === 'ok' ? 'success' : 'danger'" dot>
                            {{ i18n.t("credentials.verify." + verified.status) }}
                          </z-badge>
                          <br /><span class="z-muted caption">{{
                            i18n.date(verified.at, "short")
                          }}</span>
                          @if (verified.message) {
                            <br /><span class="z-muted caption">{{ verified.message }}</span>
                          }
                        } @else {
                          <span class="z-subtle">{{ i18n.t("credentials.notVerified") }}</span>
                        }
                      </td>
                      <td class="z-mono">{{ row.consumers }}</td>
                    </tr>
                  }
                </tbody>
              </table>
            </z-table-container>
          }
        </z-panel>
      }
    </div>
  `,
  styles: `
    .pad {
      padding: var(--space-4);
    }
  `,
})
export class CredentialsPage {
  protected readonly i18n = inject(I18n);
  protected readonly store = inject(CredentialsStore);

  protected readonly error = computed(() => {
    const problem = this.store.error();
    return this.store.state() === "error" && problem ? problemText(this.i18n, problem) : "";
  });

  protected readonly rows = computed(() =>
    this.store.instances().map((instance: CredentialInstance) => ({
      instance,
      origin:
        instance.origin === "user"
          ? this.i18n.t("origin.user")
          : this.i18n.t("origin.profile", { name: profileName(instance.origin) }),
      expiry: this.expiry(instance),
      consumers: Object.keys(instance.consumers).sort().join(", ") || "–",
    })),
  );

  constructor() {
    void this.store.ensureLoaded();
  }

  protected reload(): void {
    void this.store.reload();
  }

  private expiry(instance: CredentialInstance): string {
    const expires = instance.secret?.expiresAt;
    if (expires) {
      return this.i18n.t("credentials.expires", { date: this.i18n.date(expires, "short") });
    }
    return instance.secret?.updatedAt
      ? this.i18n.t("credentials.setOn", {
          date: this.i18n.date(instance.secret.updatedAt, "short"),
        })
      : this.i18n.t("credentials.noExpiry");
  }
}
