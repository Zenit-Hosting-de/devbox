import { DOCUMENT } from "@angular/common";
import {
  ChangeDetectionStrategy,
  Component,
  afterNextRender,
  computed,
  effect,
  inject,
  signal,
} from "@angular/core";
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from "@angular/router";
import { filter } from "rxjs";
import {
  ZAlert,
  ZAppHeader,
  ZBrand,
  ZHeaderEnd,
  ZSelect,
  ZSidebar,
  ZSidebarGroup,
  ZSidebarItem,
  ZSkipLink,
  ZTheme,
  ZToast,
  ZToastOutlet,
} from "zenit-ui";
import { SCHEMES } from "../settings/appearance";
import { I18n, LANGUAGES, isLanguage } from "../i18n/i18n";
import { PluginMessagesStore } from "../i18n/plugin-messages";
import { PluginText } from "../i18n/plugin-text";
import { InfoStore } from "../stores/info.store";
import { RestartStore } from "../stores/restart.store";
import { SessionsStore } from "../stores/sessions.store";
import { Dock } from "./dock";

interface NavItem {
  path: string;
  icon: string;
  label: string;
  exact: boolean;
}

const NAV: readonly { path: string; icon: string; key: string; exact: boolean }[] = [
  { path: "/", icon: "dashboard", key: "nav.overview", exact: true },
  { path: "/plugins", icon: "extension", key: "nav.plugins", exact: false },
  { path: "/verlauf", icon: "history", key: "nav.history", exact: false },
  { path: "/einstellungen", icon: "tune", key: "nav.settings", exact: false },
  { path: "/zugaenge", icon: "key", key: "nav.credentials", exact: false },
];

@Component({
  selector: "dbx-root",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    Dock,
    RouterLink,
    RouterLinkActive,
    RouterOutlet,
    ZAlert,
    ZAppHeader,
    ZBrand,
    ZHeaderEnd,
    ZSelect,
    ZSidebar,
    ZSidebarGroup,
    ZSidebarItem,
    ZSkipLink,
    ZToastOutlet,
  ],
  template: `
    <a zSkipLink class="body-sm" href="#inhalt">{{ i18n.t("shell.skip") }}</a>

    <div class="app">
      <div class="z-container">
        <z-app-header [navLabel]="i18n.t('shell.mainNav')">
          <a zBrand routerLink="/">DevBox</a>
          <z-select zHeaderEnd size="sm">
            <select [attr.aria-label]="i18n.t('appearance.language')" (change)="onLanguage($event)">
              @for (l of languages; track l.code) {
                <option [value]="l.code" [selected]="l.code === i18n.language()">
                  {{ l.name }}
                </option>
              }
            </select>
          </z-select>
          <z-select zHeaderEnd size="sm">
            <select [attr.aria-label]="i18n.t('appearance.theme')" (change)="onScheme($event)">
              @for (scheme of schemes; track scheme) {
                <option [value]="scheme" [selected]="scheme === theme.scheme()">
                  {{ i18n.t("theme." + scheme) }}
                </option>
              }
            </select>
          </z-select>
        </z-app-header>
      </div>

      @if (restart.restarting()) {
        <div class="z-container offline">
          <z-alert status="info" icon="restart_alt" [title]="i18n.t('shell.restarting')">
            {{ i18n.t("shell.restartingHint") }}
          </z-alert>
        </div>
      } @else if (sessions.status() === "offline") {
        <div class="z-container offline">
          <z-alert status="warning" icon="cloud_off" [title]="i18n.t('shell.offline')">
            {{ i18n.t("shell.offlineHint") }}
          </z-alert>
        </div>
      }

      <div class="z-container content">
        <div class="z-panel-shell">
          <z-sidebar [ariaLabel]="i18n.t('shell.sideNav')">
            <z-sidebar-group>
              @for (item of nav(); track item.path) {
                <a
                  zSidebarItem
                  [icon]="item.icon"
                  [routerLink]="item.path"
                  routerLinkActive
                  #link="routerLinkActive"
                  [routerLinkActiveOptions]="{ exact: item.exact }"
                  [active]="link.isActive"
                  >{{ item.label }}</a
                >
              }
            </z-sidebar-group>
          </z-sidebar>
          <main id="inhalt" tabindex="-1" class="main">
            <router-outlet />
          </main>
        </div>
      </div>

      <dbx-dock />
    </div>

    <z-toast-outlet />
    <div
      id="announcer"
      class="z-visually-hidden"
      role="status"
      aria-live="polite"
      aria-atomic="true"
    >
      {{ announcement() }}
    </div>
  `,
  styles: `
    .app {
      display: flex;
      flex-direction: column;
      min-block-size: 100dvh;
    }
    .content {
      flex: 1;
      inline-size: 100%;
      padding-block: var(--space-5);
    }
    .main {
      min-inline-size: 0;
    }
    .offline {
      padding-block-start: var(--space-3);
    }
  `,
})
export class App {
  protected readonly i18n = inject(I18n);
  protected readonly sessions = inject(SessionsStore);
  protected readonly restart = inject(RestartStore);
  protected readonly theme = inject(ZTheme);
  private readonly doc = inject(DOCUMENT);
  private readonly router = inject(Router);
  private readonly toast = inject(ZToast);
  private readonly text = inject(PluginText);

  protected readonly languages = LANGUAGES;
  protected readonly schemes = SCHEMES;

  private readonly titleKey = signal("nav.overview");

  protected readonly nav = computed<NavItem[]>(() =>
    NAV.map((n) => ({ path: n.path, icon: n.icon, exact: n.exact, label: this.i18n.t(n.key) })),
  );

  /** Text for the live region: a session started or ended. */
  protected readonly announcement = computed(() => {
    const transition = this.sessions.transition();
    if (!transition) {
      return "";
    }
    const { session } = transition;
    const [plugin, action] = session.action.split(".");
    const title =
      plugin && action ? this.text.actionLabel(plugin, action, session.title) : session.action;
    return transition.kind === "started"
      ? this.i18n.t("announce.started", { title })
      : this.i18n.t("announce.ended", {
          title,
          state: this.i18n.t(`session.state.${session.state}`),
        });
  });

  constructor() {
    // Instantiated here so translation files are fetched as soon as plugins are known.
    inject(PluginMessagesStore);
    const info = inject(InfoStore);

    this.sessions.on("error", (message) => {
      this.toast.error(
        message.detail
          ? `${this.i18n.t(`problem.${message.code}`)} ${message.detail}`
          : this.i18n.t(`problem.${message.code}`),
      );
    });

    // The socket is only opened when the core announces it; without /info (core not
    // reachable) the store still connects and shows the offline state.
    afterNextRender(() => {
      void info.load().then((loaded) => {
        if (loaded === null || loaded.features?.includes("ws")) {
          this.sessions.connect();
        }
      });
    });

    this.router.events.pipe(filter((e) => e instanceof NavigationEnd)).subscribe(() => {
      let route = this.router.routerState.snapshot.root;
      while (route.firstChild) {
        route = route.firstChild;
      }
      const key = route.data["title"];
      this.titleKey.set(typeof key === "string" ? key : "nav.overview");
    });

    effect(() => {
      this.doc.title = `${this.i18n.t(this.titleKey())} – DevBox`;
    });
  }

  protected onLanguage(event: Event): void {
    const value = (event.target as HTMLSelectElement).value;
    if (isLanguage(value)) {
      void this.i18n.setLanguage(value);
    }
  }

  protected onScheme(event: Event): void {
    this.theme.setScheme((event.target as HTMLSelectElement).value);
  }
}
