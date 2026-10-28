import { Injectable, effect, inject, signal, untracked } from "@angular/core";
import { ApiClient } from "../api/api-client";
import type { Messages, PluginDetail } from "../api/types";
import { PluginsStore } from "../stores/plugins.store";
import { I18n } from "./i18n";

/** Translation files of the plugins (GET /plugins/{id}/messages/{lang}), cached per language. */
@Injectable({ providedIn: "root" })
export class PluginMessagesStore {
  private readonly api = inject(ApiClient);
  private readonly i18n = inject(I18n);
  private readonly plugins = inject(PluginsStore);

  private readonly cache = signal<ReadonlyMap<string, Messages>>(new Map());
  private readonly requested = new Set<string>();

  constructor() {
    // Whenever the plugin list or the language changes, the matching files are fetched.
    effect(() => {
      const plugins = this.plugins.plugins();
      const language = this.i18n.language();
      untracked(() => {
        this.ensure(plugins, language);
      });
    });
  }

  /** Fetches the files that are not cached yet; a missing file (404) counts as empty. */
  ensure(plugins: readonly Pick<PluginDetail, "id">[], language: string): Promise<void> {
    const tasks: Promise<void>[] = [];
    for (const { id } of plugins) {
      const key = `${id}/${language}`;
      if (this.requested.has(key)) {
        continue;
      }
      this.requested.add(key);
      tasks.push(this.fetch(id, language, key));
    }
    return Promise.all(tasks).then(() => undefined);
  }

  /** The translated text of a plugin, or undefined when it has none. */
  lookup(plugin: string, key: string): string | undefined {
    const text = this.cache().get(`${plugin}/${this.i18n.language()}`)?.[key];
    return text === undefined || text === "" ? undefined : text;
  }

  private async fetch(plugin: string, language: string, key: string): Promise<void> {
    let messages: Messages = {};
    try {
      messages = await this.api.pluginMessages(plugin, language);
    } catch {
      // 404 (no translation file) and failures alike: the manifest text is shown.
    }
    this.cache.update((map) => new Map(map).set(key, messages));
  }
}
