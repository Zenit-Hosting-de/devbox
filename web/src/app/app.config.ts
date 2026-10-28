import { provideHttpClient, withInterceptors } from "@angular/common/http";
import {
  provideAppInitializer,
  provideZonelessChangeDetection,
  inject,
  type ApplicationConfig,
  type Provider,
} from "@angular/core";
import { provideRouter, withComponentInputBinding } from "@angular/router";
import {
  Z_LABELS,
  Z_LABELS_DE,
  Z_LABELS_EN,
  ZTheme,
  provideZenitTheme,
  type ZLabels,
} from "zenit-ui";
import { backendInterceptors, backendProviders } from "../environments/environment";
import { apiInterceptor } from "./api/interceptor";
import { routes } from "./app.routes";
import { STORAGE_KEYS, readStored, removeStored } from "./core/storage";
import { I18n } from "./i18n/i18n";
import { InfoStore } from "./stores/info.store";

/**
 * The labels of the zenit-ui components follow the UI language. The registry is an object of
 * getters, so a component created after a language switch reads the new texts at once.
 */
export function liveZenitLabels(language: () => string): ZLabels {
  const labels = {} as ZLabels;
  for (const key of Object.keys(Z_LABELS_DE) as (keyof ZLabels)[]) {
    Object.defineProperty(labels, key, {
      enumerable: true,
      get: () => (language() === "de" ? Z_LABELS_DE : Z_LABELS_EN)[key],
    });
  }
  return labels;
}

const zenitLabels: Provider = {
  provide: Z_LABELS,
  useFactory: () => {
    const i18n = inject(I18n);
    return liveZenitLabels(() => i18n.language());
  },
};

/**
 * Before the first render: read /info once and let the configured language and colour scheme
 * be the initial choice, unless this browser already has its own. A failing /info is not
 * fatal; the pages then show the error.
 */
export function initialAppearance(): Promise<void> {
  const info = inject(InfoStore);
  const i18n = inject(I18n);
  const theme = inject(ZTheme);
  return info.load().then(async (loaded) => {
    const ui = loaded?.ui;
    await i18n.applyConfigured(ui?.language);
    await i18n.ensureLoaded(i18n.language());
    if (ui?.scheme && readStored(STORAGE_KEYS.theme) === null) {
      theme.setScheme(ui.scheme);
      // The configured scheme is a default, not the viewer's choice.
      removeStored(STORAGE_KEYS.theme);
    }
    if (ui?.accent && readStored(STORAGE_KEYS.theme) === null) {
      theme.setAccent(ui.accent);
      removeStored(STORAGE_KEYS.theme);
    }
  });
}

export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    provideRouter(routes, withComponentInputBinding()),
    provideHttpClient(withInterceptors([apiInterceptor, ...backendInterceptors])),
    provideZenitTheme({ defaultScheme: "system", storageKey: STORAGE_KEYS.theme }),
    zenitLabels,
    ...backendProviders,
    provideAppInitializer(initialAppearance),
  ],
};
