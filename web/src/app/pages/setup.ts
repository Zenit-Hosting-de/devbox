import { ChangeDetectionStrategy, Component, computed, inject, signal } from "@angular/core";
import {
  ZAlert,
  ZAlertAction,
  ZBadge,
  ZButton,
  ZEmptyState,
  ZIcon,
  ZPageHeader,
  ZPanel,
  ZPanelActions,
  ZSkeleton,
  type ZBadgeStatus,
} from "zenit-ui";
import { ActionLauncher } from "../actions/action-launcher";
import { ApiClient } from "../api/api-client";
import { toApiProblem } from "../api/problem";
import { isLive, type Group, type SetupPlan, type SetupStep } from "../api/types";
import { layerName, problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { SessionsStore } from "../stores/sessions.store";

type State = "loading" | "ready" | "error";

const STEP_BADGE: Record<SetupStep["state"], ZBadgeStatus> = {
  done: "success",
  open: "warning",
  blocked: "danger",
};

/** The plan of the first-run setup with the state of every step, and the action groups. */
@Component({
  selector: "dbx-setup-page",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ZAlert,
    ZAlertAction,
    ZBadge,
    ZButton,
    ZEmptyState,
    ZIcon,
    ZPageHeader,
    ZPanel,
    ZPanelActions,
    ZSkeleton,
  ],
  template: `
    <z-page-header [title]="i18n.t('nav.setup')" [sub]="i18n.t('setup.sub')" />

    <div class="z-stack">
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
        <z-panel [title]="i18n.t('setup.planTitle')" headingLevel="2">
          <div zPanelActions class="z-cluster">
            @if (next(); as step) {
              <button zBtn="primary" size="sm" type="button" (click)="run(step.ref)">
                <z-icon name="play_arrow" />{{ i18n.t("setup.runNext") }}
              </button>
            }
          </div>
          <div class="z-stack">
            <p class="z-muted">
              {{ i18n.t("setup.origin", { origin: originText() }) }}
              {{ i18n.t("setup.progress", { done: doneCount(), count: steps().length }) }}
            </p>
            @if (steps().length === 0) {
              <z-empty-state [title]="i18n.t('setup.empty')">
                {{ i18n.t("setup.emptyHint") }}
              </z-empty-state>
            } @else {
              <ol class="steps">
                @for (step of steps(); track step.ref) {
                  <li class="step" [attr.data-step]="step.ref">
                    <div class="z-cluster">
                      <z-badge [status]="badge(step)" dot>
                        {{ i18n.t("setup.state." + step.state) }}
                      </z-badge>
                      <span>{{ step.label || step.ref }}</span>
                      <span class="z-mono z-muted">{{ step.ref }}</span>
                      @if (step.kind === "action" && step.state === "open") {
                        <button
                          zBtn="ghost"
                          size="sm"
                          type="button"
                          class="end"
                          (click)="run(step.ref)"
                        >
                          {{ i18n.t("setup.run") }}
                        </button>
                      }
                    </div>
                    @if (step.detail) {
                      <p class="z-muted detail">{{ detail(step) }}</p>
                    }
                  </li>
                }
              </ol>
            }
          </div>
        </z-panel>

        <z-panel [title]="i18n.t('setup.groupsTitle')" headingLevel="2">
          @if (groups().length === 0) {
            <z-empty-state [title]="i18n.t('setup.noGroups')">
              {{ i18n.t("setup.noGroupsHint") }}
            </z-empty-state>
          } @else {
            <div class="z-stack">
              @for (group of groups(); track group.id) {
                <section class="z-stack group" [attr.data-group]="group.id">
                  <div class="z-cluster">
                    <h3 class="title">{{ group.label || group.id }}</h3>
                    <span class="z-mono z-muted">{{ group.id }}</span>
                  </div>
                  <div class="z-cluster">
                    @for (action of group.actions; track action) {
                      <button zBtn="secondary" size="sm" type="button" (click)="run(action)">
                        <z-icon name="play_arrow" />{{ action }}
                      </button>
                    }
                  </div>
                </section>
              }
            </div>
          }
        </z-panel>
      }
    </div>
  `,
  styles: `
    .steps {
      margin: 0;
      padding-inline-start: var(--space-5, 1.25rem);
    }
    .step + .step {
      margin-block-start: var(--space-3, 0.75rem);
    }
    .detail {
      margin: 0;
    }
    .end {
      margin-inline-start: auto;
    }
    .title {
      margin: 0;
      font-size: var(--z-text-md, 1rem);
    }
  `,
})
export class SetupPage {
  protected readonly i18n = inject(I18n);
  private readonly api = inject(ApiClient);
  private readonly launcher = inject(ActionLauncher);

  protected readonly state = signal<State>("loading");
  protected readonly error = signal("");
  private readonly plan = signal<SetupPlan>({ origin: "plugins", steps: [] });
  protected readonly groups = signal<readonly Group[]>([]);

  protected readonly steps = computed(() => this.plan().steps);
  protected readonly doneCount = computed(
    () => this.steps().filter((s) => s.state === "done").length,
  );
  /** The first open action step: what "run the next step" starts. */
  protected readonly next = computed(() =>
    this.steps().find((s) => s.kind === "action" && s.state === "open"),
  );
  protected readonly originText = computed(() => {
    const origin = this.plan().origin;
    return origin === "plugins" ? this.i18n.t("setup.originPlugins") : layerName(this.i18n, origin);
  });

  constructor() {
    const sessions = inject(SessionsStore);
    // a finished session can finish a step
    sessions.on("session", (message) => {
      if (!isLive(message.session.state) && this.state() === "ready") {
        void this.load();
      }
    });
    sessions.onReconnect(() => this.load());
    void this.load();
  }

  protected badge(step: SetupStep): ZBadgeStatus {
    return STEP_BADGE[step.state];
  }

  /** The core explains a waiting credential step in English; the page uses its own text. */
  protected detail(step: SetupStep): string {
    return step.kind === "credential" && step.state === "open"
      ? this.i18n.t("setup.secretMissing")
      : (step.detail ?? "");
  }

  async load(): Promise<void> {
    if (this.state() !== "ready") {
      this.state.set("loading");
    }
    try {
      const [plan, groups] = await Promise.all([this.api.setup(), this.api.groups()]);
      this.plan.set(plan);
      this.groups.set(groups);
      this.state.set("ready");
    } catch (error) {
      this.error.set(problemText(this.i18n, toApiProblem(error)));
      this.state.set("error");
    }
  }

  /** Opens the action dialog: parameters, dry run and confirmation work as everywhere else. */
  protected run(ref: string): void {
    void this.launcher.start(ref);
  }
}
