// TEMPORARY CONTRACT DRAFT (AP-14).
//
// api/openapi.json on main does not yet contain the web UI contract commit of the
// AP-09 branch (claude/ap-09-api). This file holds the types openapi-typescript derives
// from that contract as specified, so the UI can be built and type-checked meanwhile.
// It is imported only by ./types.ts. Once AP-09 is merged: change the single import in
// ./types.ts to ../gen/api.js and delete this file.

export interface paths {
  "/info": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** Version, host, authentication mode, protocol version and features. */
    get: operations["getInfo"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/plugins": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** Plugins with state and the reasons for it. */
    get: operations["listPlugins"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/plugins/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** One plugin with state, reasons and manifest. */
    get: operations["getPlugin"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/checks": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** Cached check results of all plugins. */
    get: operations["listChecks"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/checks/refresh": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    /** Run checks again; an empty body refreshes all checks. */
    post: operations["refreshChecks"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/settings": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["listSettings"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    /** Set user values; null removes the user value. */
    patch: operations["patchSettings"];
    trace?: never;
  };
  "/settings/{key}/explain": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** Effective value of a setting and every layer it was merged from. */
    get: operations["explainSetting"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/sessions": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["listSessions"];
    put?: never;
    /** Start an action. */
    post: operations["createSession"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/credentials/{name}/secret": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    /** Set the secret of a credential instance; starts verify and apply as a session. */
    put: operations["putCredentialSecret"];
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/schema/{name}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    /** An embedded JSON Schema, for editors and forms. */
    get: operations["getSchema"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/plugins/{id}/messages/{lang}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["getPluginMessages"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/checks/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["getCheck"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/credentials": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["listCredentials"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/sessions/{id}": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["getSession"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/sessions/{id}/events": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["listSessionEvents"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/sessions/{id}/log": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["getSessionLog"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/sessions/{id}/cancel": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get?: never;
    put?: never;
    post: operations["cancelSession"];
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
  "/ws": {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    get: operations["openWebSocket"];
    put?: never;
    post?: never;
    delete?: never;
    options?: never;
    head?: never;
    patch?: never;
    trace?: never;
  };
}
export type webhooks = Record<string, never>;
export interface components {
  schemas: {
    /** @description Identifier of a plugin, credential instance, repository or schema. */
    Name: string;
    /** @description Fully qualified check or action, <plugin>.<name>. */
    Ref: string;
    /** @description Fully qualified setting key, <plugin>.<name>. */
    SettingKey: string;
    /**
     * @description Result state of a check.
     * @enum {unknown}
     */
    CheckStatus: "ok" | "warn" | "error" | "missing";
    /**
     * @description State of a session.
     * @enum {unknown}
     */
    SessionState: "queued" | "running" | "success" | "failed" | "blocked" | "stopped" | "timeout";
    /** @description Facts about the machine. */
    Host: {
      /** @description Host name. */
      name: string;
      /** @description User the core runs as. */
      user: string;
      /** @description Distribution ID, e.g. ubuntu. */
      distro: string;
      /** @description Distribution version, e.g. 24.04. */
      distroVersion: string;
      /**
       * @description CPU architecture.
       * @enum {unknown}
       */
      arch?: "amd64" | "arm64";
      codename?: string;
      desktop?: string;
    };
    /** @description Version, host, authentication mode, protocol version and features of the core. */
    Info: {
      /**
       * @description Always zenit-hosting; distinguishes DevBox from other tools named devbox.
       * @constant
       */
      vendor: "zenit-hosting";
      /** @description Version of the core. */
      version: string;
      /** @description Git commit of the build. */
      commit?: string;
      /** @description Build date. */
      date?: string;
      /** @description Plugin protocol version. */
      protocol: number;
      /**
       * @description Value of server.auth.
       * @enum {unknown}
       */
      auth: "local" | "pam" | "off";
      host: components["schemas"]["Host"];
      /** @description Optional features of this build. */
      features?: string[];
      ui?: {
        language?: string;
        /** @enum {unknown} */
        scheme?: "system" | "light" | "dark" | "contrast";
        accent?: string;
      };
    };
    /** @description Cached result of one check. */
    CheckSummary: {
      id: components["schemas"]["Ref"];
      status: components["schemas"]["CheckStatus"];
      /** @description One-line result. */
      summary: string;
      /**
       * Format: date-time
       * @description Time of the check.
       */
      checkedAt: string;
      label?: string;
      details?: {
        label: string;
        value: string;
      }[];
      hints?: {
        text: string;
        action?: components["schemas"]["Ref"];
      }[];
      data?: Record<string, never>;
    };
    /** @description Overview of all checks; also the output of devbox status --json. */
    StatusReport: {
      host: components["schemas"]["Host"];
      /** @description Number of checks per status. */
      summary: {
        ok: number;
        warn: number;
        error: number;
        missing: number;
      };
      checks: components["schemas"]["CheckSummary"][];
      running?: components["schemas"]["Ref"][];
    };
    /** @description Checks to run again; without ids all checks run. */
    ChecksRefresh: {
      ids?: components["schemas"]["Ref"][];
    };
    /** @description Value of a setting in one configuration layer. */
    SettingLayer: {
      /** @description plugin, system, profile:<id>, user, env or call. */
      layer: string;
      /** @description Value in this layer. */
      value: unknown;
      /** @description The profile locks the value. */
      locked?: boolean;
    };
    /** @description Effective value of a setting and every layer it was merged from; also the output of devbox config explain --json. */
    SettingExplain: {
      key: components["schemas"]["SettingKey"];
      /** @description Effective value. */
      value: unknown;
      /** @description Layer the effective value comes from. */
      origin: string;
      /** @description A profile locks the value. */
      locked?: boolean;
      layers: components["schemas"]["SettingLayer"][];
    };
    /** @description User values to set; null removes the user value. */
    SettingsPatch: Record<string, never>;
    /** @description Request to start an action. */
    SessionCreate: {
      action: components["schemas"]["Ref"];
      /** @description Action parameters. */
      params?: Record<string, never>;
      /** @description Report planned changes only. */
      dryRun?: boolean;
      /** @description Terminal width. */
      cols?: number;
      /** @description Terminal height. */
      rows?: number;
      yes?: boolean;
    };
    /** @description Summary of a session. */
    Session: {
      id: components["schemas"]["SessionId"];
      action: components["schemas"]["Ref"];
      /** @description Label of the action. */
      title?: string;
      state: components["schemas"]["SessionState"];
      /** @description The session is a dry run. */
      dryRun: boolean;
      /**
       * Format: date-time
       * @description Start time.
       */
      startedAt?: string;
      /**
       * Format: date-time
       * @description End time.
       */
      endedAt?: string;
      plugin?: components["schemas"]["Name"];
      group?: components["schemas"]["Name"];
      user?: string;
      origin?: components["schemas"]["SessionOrigin"];
      exitCode?: number | null;
      changes?: number;
      summary?: string;
      progress?: {
        percent: number | null;
        text?: string;
      };
      nextSeq?: number;
    };
    /** @description New secret of a credential instance. */
    SecretPut: {
      /** @description Token or password; never returned by the API. */
      secret: string;
      /**
       * Format: date-time
       * @description Known expiry, or null.
       */
      expiresAt?: string | null;
    };
    /** @description One field error of a problem. */
    ProblemError: {
      /** @description JSON Pointer of the field in the request body. */
      pointer: string;
      /** @description Stable error code of the field. */
      code: string;
      /** @description Readable message. */
      message?: string;
    };
    /** @description Error in RFC 9457 format with a stable code. */
    Problem: {
      /**
       * Format: uri
       * @description URI of the error type.
       */
      type: string;
      /** @description Short, readable summary. */
      title: string;
      /** @description HTTP status code. */
      status: number;
      /**
       * @description Stable error code.
       * @enum {unknown}
       */
      code:
        | "validation"
        | "not-found"
        | "plugin-not-ready"
        | "setting-locked"
        | "permission-denied"
        | "unauthenticated"
        | "csrf"
        | "conflict"
        | "internal";
      /** @description Readable explanation of this occurrence. */
      detail?: string;
      /** @description Request path. */
      instance?: string;
      /** @description Log reference for internal errors. */
      traceId?: string;
      errors?: components["schemas"]["ProblemError"][];
    };
    /**
     * @description State of a plugin; only ready plugins offer actions.
     * @enum {unknown}
     */
    PluginState: "invalid" | "blocked" | "disabled" | "incomplete" | "ready";
    /** @description Why a plugin is not ready, or a note about it. code is stable and translated by the frontends. */
    PluginReason: {
      /**
       * @description Reason code.
       * @enum {unknown}
       */
      code:
        | "manifest-invalid"
        | "disabled"
        | "requires-devbox"
        | "requires-os"
        | "requires-arch"
        | "requires-distro"
        | "requires-command"
        | "requires-plugin"
        | "dependency-cycle"
        | "setting-missing"
        | "duplicate-id";
      /** @description Subject of the reason: command, plugin and range, setting key, disabling layer or error location. */
      detail?: string;
    };
    /** @description A found plugin with its state. */
    PluginStatus: {
      id: components["schemas"]["Name"];
      /** @description Display name from the manifest. */
      name?: string;
      /** @description Plugin version. */
      version?: string;
      /** @description Category from the manifest. */
      category?: string;
      state: components["schemas"]["PluginState"];
      reasons: components["schemas"]["PluginReason"][];
      /** @description Plugin directory. */
      path: string;
      /** @description Directories of later plugins with the same ID that this one replaces. */
      shadowed?: string[];
    };
    /** @description All found plugins, sorted by ID. */
    PluginList: components["schemas"]["PluginDetail"][];
    /** @description A plugin with its manifest (absent when the manifest is invalid). */
    PluginDetail: {
      id: components["schemas"]["Name"];
      /** @description Display name from the manifest. */
      name?: string;
      /** @description Plugin version. */
      version?: string;
      /** @description Category from the manifest. */
      category?: string;
      state: components["schemas"]["PluginState"];
      reasons: components["schemas"]["PluginReason"][];
      /** @description Plugin directory. */
      path: string;
      /** @description Directories of later plugins with the same ID that this one replaces. */
      shadowed?: string[];
      /** @description The plugin manifest (schema/plugin.v1.json). */
      manifest?: Record<string, never>;
      resolvedDefaults?: {
        [key: string]: Record<string, never>;
      };
    };
    /** @description Session ID. */
    SessionId: string;
    Messages: {
      [key: string]: string;
    };
    SettingItem: {
      key: components["schemas"]["SettingKey"];
      plugin: components["schemas"]["Name"];
      field: Record<string, never>;
      value?: unknown;
      origin?: string;
      locked: boolean;
      lockedBy?: string;
    };
    SettingList: {
      settings: components["schemas"]["SettingItem"][];
    };
    CredentialInstance: {
      name: components["schemas"]["Name"];
      type: string;
      origin: string;
      label?: string;
      fields: {
        [key: string]: string | number | boolean;
      };
      locked: string[];
      consumers: {
        [key: string]: Record<string, never>;
      };
      secret?: {
        ref: string;
        /** Format: date-time */
        updatedAt?: string;
        /** Format: date-time */
        expiresAt?: string | null;
      };
      verified?: {
        /** Format: date-time */
        at: string;
        /** @enum {unknown} */
        status: "ok" | "failed";
        message?: string;
      };
      applied: {
        [key: string]: string;
      };
    };
    CredentialList: {
      instances: components["schemas"]["CredentialInstance"][];
    };
    /** @enum {unknown} */
    SessionOrigin: "ui" | "cli" | "tui" | "api" | "setup" | "group" | "event";
    SessionList: {
      sessions: components["schemas"]["Session"][];
      next?: string;
    };
    SessionEvent: {
      seq: number;
      /** Format: date-time */
      time: string;
      event: {
        type: string;
      };
    };
    SessionEventList: {
      events: components["schemas"]["SessionEvent"][];
    };
    WsSubscribe: {
      /** @constant */
      type: "subscribe";
      topics: string[];
      fromSeq?: number;
    };
    WsUnsubscribe: {
      /** @constant */
      type: "unsubscribe";
      topics: string[];
    };
    WsInput: {
      /** @constant */
      type: "input";
      session: components["schemas"]["SessionId"];
      data: string;
    };
    WsResize: {
      /** @constant */
      type: "resize";
      session: components["schemas"]["SessionId"];
      cols: number;
      rows: number;
    };
    WsPing: {
      /** @constant */
      type: "ping";
    };
    WsPong: {
      /** @constant */
      type: "pong";
    };
    WsClientMessage:
      | components["schemas"]["WsSubscribe"]
      | components["schemas"]["WsUnsubscribe"]
      | components["schemas"]["WsInput"]
      | components["schemas"]["WsResize"]
      | components["schemas"]["WsPing"]
      | components["schemas"]["WsPong"];
    WsOutput: {
      /** @constant */
      type: "output";
      session: components["schemas"]["SessionId"];
      seq: number;
      data: string;
    };
    WsEvent: {
      /** @constant */
      type: "event";
      session: components["schemas"]["SessionId"];
      seq: number;
      /** Format: date-time */
      time: string;
      event: Record<string, never>;
    };
    WsSession: {
      /** @constant */
      type: "session";
      session: components["schemas"]["Session"];
    };
    WsCheck: {
      /** @constant */
      type: "check";
      id: components["schemas"]["Ref"];
      result: components["schemas"]["CheckSummary"];
    };
    WsSettings: {
      /** @constant */
      type: "settings";
      keys: components["schemas"]["SettingKey"][];
    };
    WsGap: {
      /** @constant */
      type: "gap";
      session: components["schemas"]["SessionId"];
      fromSeq: number;
      firstSeq: number;
    };
    WsError: {
      /** @constant */
      type: "error";
      /**
       * @description Stable error code.
       * @enum {unknown}
       */
      code:
        | "validation"
        | "not-found"
        | "plugin-not-ready"
        | "setting-locked"
        | "permission-denied"
        | "unauthenticated"
        | "csrf"
        | "conflict"
        | "internal";
      detail?: string;
      topic?: string;
    };
    WsServerMessage:
      | components["schemas"]["WsOutput"]
      | components["schemas"]["WsEvent"]
      | components["schemas"]["WsSession"]
      | components["schemas"]["WsCheck"]
      | components["schemas"]["WsSettings"]
      | components["schemas"]["WsGap"]
      | components["schemas"]["WsError"]
      | components["schemas"]["WsPing"]
      | components["schemas"]["WsPong"];
  };
  responses: {
    /** @description Error in RFC 9457 format. */
    Problem: {
      headers: {
        [name: string]: unknown;
      };
      content: {
        "application/problem+json": components["schemas"]["Problem"];
      };
    };
  };
  parameters: {
    /** @description Required on every modifying request (CSRF protection), together with a matching Origin. */
    DevBoxRequest: "1";
  };
  requestBodies: never;
  headers: never;
  pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
  getInfo: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Information about the core. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Info"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  listPlugins: {
    parameters: {
      query?: {
        manifest?: boolean;
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description All found plugins, sorted by ID. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["PluginList"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  getPlugin: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        /** @description Plugin ID. */
        id: components["schemas"]["Name"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description The plugin. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["PluginDetail"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  listChecks: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Status report with all cached check results. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["StatusReport"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  refreshChecks: {
    parameters: {
      query?: never;
      header: {
        /** @description Required on every modifying request (CSRF protection), together with a matching Origin. */
        "X-DevBox-Request": components["parameters"]["DevBoxRequest"];
      };
      path?: never;
      cookie?: never;
    };
    requestBody?: {
      content: {
        "application/json": components["schemas"]["ChecksRefresh"];
      };
    };
    responses: {
      /** @description The checks are running; results arrive over the WebSocket topic "checks". */
      202: {
        headers: {
          [name: string]: unknown;
        };
        content?: never;
      };
      default: components["responses"]["Problem"];
    };
  };
  listSettings: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Settings. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["SettingList"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  patchSettings: {
    parameters: {
      query?: never;
      header: {
        /** @description Required on every modifying request (CSRF protection), together with a matching Origin. */
        "X-DevBox-Request": components["parameters"]["DevBoxRequest"];
      };
      path?: never;
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["SettingsPatch"];
      };
    };
    responses: {
      /** @description The values were stored. */
      204: {
        headers: {
          [name: string]: unknown;
        };
        content?: never;
      };
      default: components["responses"]["Problem"];
    };
  };
  explainSetting: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        /** @description Fully qualified setting key, e.g. java.vendorApi. */
        key: components["schemas"]["SettingKey"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Origin of the effective value. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["SettingExplain"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  listSessions: {
    parameters: {
      query?: {
        state?: components["schemas"]["SessionState"];
        plugin?: components["schemas"]["Name"];
        limit?: number;
        cursor?: string;
      };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Sessions. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["SessionList"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  createSession: {
    parameters: {
      query?: never;
      header: {
        /** @description Required on every modifying request (CSRF protection), together with a matching Origin. */
        "X-DevBox-Request": components["parameters"]["DevBoxRequest"];
      };
      path?: never;
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["SessionCreate"];
      };
    };
    responses: {
      /** @description The session was created. */
      201: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Session"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  putCredentialSecret: {
    parameters: {
      query?: never;
      header: {
        /** @description Required on every modifying request (CSRF protection), together with a matching Origin. */
        "X-DevBox-Request": components["parameters"]["DevBoxRequest"];
      };
      path: {
        /** @description Name of the credential instance. */
        name: components["schemas"]["Name"];
      };
      cookie?: never;
    };
    requestBody: {
      content: {
        "application/json": components["schemas"]["SecretPut"];
      };
    };
    responses: {
      /** @description Verify and apply run as a session. */
      202: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Session"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  getSchema: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        /** @description Schema name without version suffix, e.g. config. */
        name: components["schemas"]["Name"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description The JSON Schema document. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/schema+json": Record<string, never>;
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  getPluginMessages: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["Name"];
        lang: string;
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Messages. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Messages"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  getCheck: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["Ref"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Check. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["CheckSummary"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  listCredentials: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Credentials. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["CredentialList"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  getSession: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["SessionId"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Session. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Session"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  listSessionEvents: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["SessionId"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Events. */
      200: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["SessionEventList"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  getSessionLog: {
    parameters: {
      query?: never;
      header?: never;
      path: {
        id: components["schemas"]["SessionId"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Raw terminal output. */
      200: {
        headers: {
          "X-DevBox-Next-Seq"?: number;
          [name: string]: unknown;
        };
        content: {
          "application/octet-stream": string;
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  cancelSession: {
    parameters: {
      query?: never;
      header: {
        /** @description Required on every modifying request (CSRF protection), together with a matching Origin. */
        "X-DevBox-Request": components["parameters"]["DevBoxRequest"];
      };
      path: {
        id: components["schemas"]["SessionId"];
      };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Session. */
      202: {
        headers: {
          [name: string]: unknown;
        };
        content: {
          "application/json": components["schemas"]["Session"];
        };
      };
      default: components["responses"]["Problem"];
    };
  };
  openWebSocket: {
    parameters: {
      query?: never;
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      /** @description Switching Protocols */
      101: {
        headers: {
          [name: string]: unknown;
        };
        content?: never;
      };
      default: components["responses"]["Problem"];
    };
  };
}
