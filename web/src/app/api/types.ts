// Contract types used by the application. Everything is derived from the OpenAPI types
// (components['schemas']); a few `object` schemas are narrowed here because the contract
// leaves them open (manifest, field, check data).

import type { components, paths } from "../gen/api";
import type { FormField } from "../schema-form/field";
import type { Message as ProtocolMessage } from "../gen/event";

export type { paths };
export type Schemas = components["schemas"];

export type Name = Schemas["Name"];
export type Ref = Schemas["Ref"];
export type SettingKey = Schemas["SettingKey"];
export type CheckStatus = Schemas["CheckStatus"];
export type SessionState = Schemas["SessionState"];
export type SessionId = Schemas["SessionId"];
export type SessionOrigin = Schemas["SessionOrigin"];
export type PluginState = Schemas["PluginState"];
export type PluginReason = Schemas["PluginReason"];
export type Host = Schemas["Host"];
export type Info = Schemas["Info"];
export type Problem = Schemas["Problem"];
export type ProblemError = Schemas["ProblemError"];
export type ProblemCode = Problem["code"];
export type Session = Schemas["Session"];
export type SessionList = Schemas["SessionList"];
/** The contract leaves `params` an open object; the UI sends typed values. */
export interface SessionCreate extends Omit<Schemas["SessionCreate"], "params"> {
  params?: Record<string, unknown>;
}
/** Consumer options are free-form objects; the contract leaves them open. */
export interface CredentialInstance extends Omit<Schemas["CredentialInstance"], "consumers"> {
  consumers: Record<string, Record<string, unknown>>;
}
export interface CredentialList {
  instances: CredentialInstance[];
}
export type Messages = Schemas["Messages"];
export type SettingLayer = Schemas["SettingLayer"];
export type StatusReportBase = Schemas["StatusReport"];
export type WsClientMessage = Schemas["WsClientMessage"];
export type WsServerMessage = Schemas["WsServerMessage"];

/** A session event as the protocol describes it (schema/event.v1.json). */
export type SessionEventPayload = ProtocolMessage;

export interface SessionEvent {
  seq: number;
  time: string;
  event: SessionEventPayload;
}

/** The contract types `event` as an open object with a type; the UI reads it as the protocol message. */
export interface SessionEventList {
  events: SessionEvent[];
}

export interface CheckSummary extends Omit<Schemas["CheckSummary"], "data"> {
  data?: Record<string, unknown>;
}

export interface StatusReport extends Omit<StatusReportBase, "checks"> {
  checks: CheckSummary[];
}

export interface SettingItem extends Omit<Schemas["SettingItem"], "field"> {
  field: FormField;
}

export interface SettingList {
  settings: SettingItem[];
}

/** The parts of the plugin manifest the UI reads (schema/plugin.v1.json). */
export interface ManifestAction {
  label: string;
  description?: string;
  params?: Record<string, FormField>;
  timeout?: string;
  confirm?: boolean | string;
  danger?: boolean;
  sudo?: boolean;
}

export interface ManifestCheck {
  label: string;
  network?: boolean;
}

export interface Manifest {
  id: string;
  version: string;
  name: string;
  description?: string;
  category?: string;
  icon?: string;
  settings?: Record<string, FormField>;
  checks?: Record<string, ManifestCheck>;
  actions?: Record<string, ManifestAction>;
  contributes?: {
    overview?: { check?: string; primaryAction?: string };
    links?: { label: string; url: string }[];
  };
  i18n?: { default?: string; dir?: string };
}

export interface PluginDetail extends Omit<
  Schemas["PluginDetail"],
  "manifest" | "resolvedDefaults"
> {
  manifest?: Manifest;
  resolvedDefaults?: Record<string, Record<string, unknown>>;
}

export type PluginList = PluginDetail[];

export type SessionsQuery = {
  state?: SessionState;
  plugin?: Name;
  limit?: number;
  cursor?: string;
};

/** Narrow the message union by its type. */
export type ServerMessageOf<T extends WsServerMessage["type"]> = Extract<
  WsServerMessage,
  { type: T }
>;

export const TERMINAL_STATES: readonly SessionState[] = [
  "success",
  "failed",
  "blocked",
  "stopped",
  "timeout",
];

export function isLive(state: SessionState): boolean {
  return state === "queued" || state === "running";
}
