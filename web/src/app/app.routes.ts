import type { Routes } from "@angular/router";
import { OverviewPage } from "./pages/overview";

/** `data.title` is a message key; the shell sets the document title from it. */
export const routes: Routes = [
  { path: "", pathMatch: "full", component: OverviewPage, data: { title: "nav.overview" } },
  {
    path: "plugins",
    loadComponent: () => import("./pages/plugins").then((m) => m.PluginsPage),
    data: { title: "nav.plugins" },
  },
  {
    path: "plugins/:id",
    loadComponent: () => import("./pages/plugin-detail").then((m) => m.PluginDetailPage),
    data: { title: "nav.plugins" },
  },
  {
    path: "verlauf",
    loadComponent: () => import("./pages/history").then((m) => m.HistoryPage),
    data: { title: "nav.history" },
  },
  {
    path: "verlauf/:id",
    loadComponent: () => import("./pages/history-detail").then((m) => m.HistoryDetailPage),
    data: { title: "nav.history" },
  },
  {
    path: "einstellungen",
    loadComponent: () => import("./pages/settings").then((m) => m.SettingsPage),
    data: { title: "nav.settings" },
  },
  {
    path: "zugaenge",
    loadComponent: () => import("./pages/credentials").then((m) => m.CredentialsPage),
    data: { title: "nav.credentials" },
  },
  // The pages have German paths; the English names redirect, so both work as links and
  // after a reload.
  { path: "history", pathMatch: "full", redirectTo: "verlauf" },
  { path: "history/:id", redirectTo: "verlauf/:id" },
  { path: "settings", pathMatch: "full", redirectTo: "einstellungen" },
  { path: "credentials", pathMatch: "full", redirectTo: "zugaenge" },
  {
    path: "**",
    loadComponent: () => import("./pages/not-found").then((m) => m.NotFoundPage),
    data: { title: "notFound.title" },
  },
];
