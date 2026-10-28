import type { ZBadgeStatus } from "zenit-ui";
import type { ApiProblem } from "../api/problem";
import type { CheckStatus, PluginState, SessionState } from "../api/types";
import type { I18n } from "../i18n/i18n";

export function checkBadge(status: CheckStatus): ZBadgeStatus {
  switch (status) {
    case "ok":
      return "success";
    case "warn":
      return "warning";
    case "error":
      return "danger";
    case "missing":
      return "info";
  }
}

export function sessionBadge(state: SessionState): ZBadgeStatus {
  switch (state) {
    case "queued":
      return "neutral";
    case "running":
      return "info";
    case "success":
      return "success";
    case "failed":
      return "danger";
    case "blocked":
    case "timeout":
      return "warning";
    case "stopped":
      return "neutral";
  }
}

export function pluginBadge(state: PluginState): ZBadgeStatus {
  switch (state) {
    case "ready":
      return "success";
    case "incomplete":
    case "blocked":
      return "warning";
    case "invalid":
      return "danger";
    case "disabled":
      return "neutral";
  }
}

/** The profile behind a lock, `profile:example-team` -> `example-team`. */
export function profileName(layer: string | undefined): string {
  return layer?.startsWith("profile:") ? layer.slice("profile:".length) : (layer ?? "");
}

/** Readable text for a failed call: the translated problem code plus the core's detail. */
export function problemText(i18n: I18n, problem: ApiProblem): string {
  const key = `problem.${problem.code}`;
  const title = i18n.has(key) ? i18n.t(key) : problem.title;
  return problem.detail && problem.detail !== title ? `${title} ${problem.detail}` : title;
}

/** Text of one field error from a problem: translated code if known, else the core's message. */
export function fieldErrorText(
  i18n: I18n,
  error: { code: string; message?: string | undefined },
): string {
  const key = `form.error.${error.code}`;
  if (i18n.has(key)) {
    return i18n.t(key);
  }
  return error.message ?? error.code;
}
