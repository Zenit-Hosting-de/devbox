/** Build-time switches. environment.mock.ts replaces environment.ts in the `mock` configuration. */
export interface Environment {
  /** The transport is replaced by fixtures (no core needed). */
  readonly mock: boolean;
  /** Path prefix of the API, relative to the address the page is served from. */
  readonly apiBase: string;
}
