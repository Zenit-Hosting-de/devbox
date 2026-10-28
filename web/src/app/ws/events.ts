import type { SessionEvent } from "../api/types";

export type StepState = "running" | "done" | "skipped" | "failed";

export interface StepView {
  id: string;
  label: string;
  state: StepState;
}

export interface ChangeView {
  kind: string;
  op: "create" | "update" | "delete";
  target: string;
  summary: string;
}

export interface NoticeView {
  kind: "relogin" | "reboot" | "restart-shell";
  text: string;
}

export interface EventSummary {
  steps: StepView[];
  progress: { percent: number | null; text?: string } | null;
  result: { summary: string } | null;
  changes: ChangeView[];
  notices: NoticeView[];
}

/** Folds the protocol events of a session into what the UI shows as text. */
export function summarizeEvents(events: readonly SessionEvent[]): EventSummary {
  const steps = new Map<string, StepView>();
  const summary: EventSummary = {
    steps: [],
    progress: null,
    result: null,
    changes: [],
    notices: [],
  };
  for (const { event } of events) {
    switch (event.type) {
      case "step":
        steps.set(event.id, { id: event.id, label: event.label, state: event.state });
        break;
      case "progress":
        summary.progress = {
          percent: event.percent,
          ...(event.text !== undefined ? { text: event.text } : {}),
        };
        break;
      case "change":
        summary.changes.push({
          kind: event.kind,
          op: event.op,
          target: event.target,
          summary: event.summary,
        });
        break;
      case "notice":
        summary.notices.push({ kind: event.kind, text: event.text });
        break;
      case "result":
        summary.result = { summary: event.summary };
        break;
      default:
        break;
    }
  }
  summary.steps = [...steps.values()];
  return summary;
}
