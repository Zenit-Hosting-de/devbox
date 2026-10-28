import { Injectable, inject } from "@angular/core";
import { ZDialog, ZToast } from "zenit-ui";
import { ApiProblem } from "../api/problem";
import type { PluginDetail, Session } from "../api/types";
import { problemText } from "../core/status";
import { I18n } from "../i18n/i18n";
import { DockStore } from "../stores/dock.store";
import { PluginsStore } from "../stores/plugins.store";
import { SessionsStore } from "../stores/sessions.store";
import type { ActionDialogData } from "./action-dialog";

export interface LaunchOptions {
  /**
   * Quick starts (primary action, hint buttons) run at once when the action has neither
   * parameters nor a confirmation; the dialog opens otherwise.
   */
  quick?: boolean;
}

/** Starts actions of plugins: through the dialog, or directly for simple quick actions. */
@Injectable({ providedIn: "root" })
export class ActionLauncher {
  private readonly dialog = inject(ZDialog);
  private readonly toast = inject(ZToast);
  private readonly plugins = inject(PluginsStore);
  private readonly sessions = inject(SessionsStore);
  private readonly dock = inject(DockStore);
  private readonly i18n = inject(I18n);

  /** `ref` is <plugin>.<action>. Resolves with the session, or undefined if none was started. */
  async start(ref: string, options: LaunchOptions = {}): Promise<Session | undefined> {
    const dot = ref.indexOf(".");
    const pluginId = ref.slice(0, dot);
    const actionName = ref.slice(dot + 1);
    await this.plugins.ensureLoaded();
    const plugin = this.plugins.plugin(pluginId);
    const action = plugin?.manifest?.actions?.[actionName];
    if (!plugin || !action) {
      this.toast.error(this.i18n.t("action.unknown", { action: ref }));
      return undefined;
    }
    const simple = Object.keys(action.params ?? {}).length === 0 && !action.confirm;
    if (options.quick && simple) {
      return this.run(plugin, actionName);
    }
    return this.openDialog(plugin, actionName);
  }

  private async run(plugin: PluginDetail, action: string): Promise<Session | undefined> {
    const size = this.dock.size();
    try {
      const session = await this.sessions.create({
        action: `${plugin.id}.${action}`,
        dryRun: false,
        cols: size.cols,
        rows: size.rows,
      });
      this.dock.openNew(session);
      return session;
    } catch (error) {
      if (error instanceof ApiProblem) {
        this.toast.error(problemText(this.i18n, error));
        return undefined;
      }
      throw error;
    }
  }

  private async openDialog(plugin: PluginDetail, action: string): Promise<Session | undefined> {
    const { ActionDialog } = await import("./action-dialog");
    const data: ActionDialogData = { plugin, action };
    const ref = this.dialog.open<Session | undefined, ActionDialogData>(ActionDialog, { data });
    return new Promise((resolve) => {
      ref.closed.subscribe((result) => {
        resolve(result);
      });
    });
  }
}
