import { Injectable, inject } from "@angular/core";
import type { PluginDetail } from "../api/types";
import { I18n } from "./i18n";
import { PluginMessagesStore } from "./plugin-messages";

/**
 * Texts that come from plugins. Lookup order: the translation file of the plugin for the
 * current language, then the text in the manifest (the plugin's default language).
 * Key scheme (plugins/<id>/i18n/<lang>.json): plugin.name, plugin.description,
 * settings.<name>.label|help, checks.<name>.label, actions.<name>.label|description|confirm,
 * actions.<name>.params.<param>.label|help.
 */
@Injectable({ providedIn: "root" })
export class PluginText {
  private readonly messages = inject(PluginMessagesStore);
  private readonly i18n = inject(I18n);

  /** Translation or fallback. */
  text(plugin: string, key: string, fallback: string): string {
    return this.messages.lookup(plugin, key) ?? fallback;
  }

  optional(plugin: string, key: string, fallback: string | undefined): string | undefined {
    return this.messages.lookup(plugin, key) ?? fallback;
  }

  pluginName(plugin: Pick<PluginDetail, "id" | "name" | "manifest">): string {
    return this.text(plugin.id, "plugin.name", plugin.manifest?.name ?? plugin.name ?? plugin.id);
  }

  pluginDescription(plugin: Pick<PluginDetail, "id" | "manifest">): string | undefined {
    return this.optional(plugin.id, "plugin.description", plugin.manifest?.description);
  }

  /** Category label from the core texts; an unknown category is shown as written. */
  category(id: string | undefined): string {
    const category = id ?? "other";
    const key = `category.${category}`;
    return this.i18n.has(key) ? this.i18n.t(key) : category;
  }

  checkLabel(plugin: string, check: string, fallback: string | undefined): string {
    return this.text(plugin, `checks.${check}.label`, fallback ?? check);
  }

  actionLabel(plugin: string, action: string, fallback: string | undefined): string {
    return this.text(plugin, `actions.${action}.label`, fallback ?? action);
  }

  actionDescription(
    plugin: string,
    action: string,
    fallback: string | undefined,
  ): string | undefined {
    return this.optional(plugin, `actions.${action}.description`, fallback);
  }

  /** The confirmation text; `true` (no text in the manifest) gives the generic question. */
  actionConfirm(plugin: string, action: string, confirm: boolean | string | undefined): string {
    if (confirm === undefined || confirm === false) {
      return "";
    }
    const fallback = typeof confirm === "string" ? confirm : this.i18n.t("action.confirm.default");
    return this.text(plugin, `actions.${action}.confirm`, fallback);
  }

  settingLabel(plugin: string, name: string, fallback: string | undefined): string {
    return this.text(plugin, `settings.${name}.label`, fallback ?? name);
  }

  settingHelp(plugin: string, name: string, fallback: string | undefined): string | undefined {
    return this.optional(plugin, `settings.${name}.help`, fallback);
  }
}
