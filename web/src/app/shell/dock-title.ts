import type { PluginText } from "../i18n/plugin-text";

/** The title of a dock tab: the action label, or the action ID while the session is unknown. */
export function dockTitle(
  text: PluginText,
  session: { action: string; title?: string | undefined } | undefined,
): string {
  if (!session) {
    return "";
  }
  const [plugin, action] = session.action.split(".");
  return plugin && action ? text.actionLabel(plugin, action, session.title) : session.action;
}
