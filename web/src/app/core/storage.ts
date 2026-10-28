// localStorage can be missing or throw (private windows, blocked site data). Every access is
// wrapped, and the application works without it.

export function readStored(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

export function writeStored(key: string, value: string): void {
  try {
    globalThis.localStorage?.setItem(key, value);
  } catch {
    // Ignored: the choice then only lives until the page is closed.
  }
}

export function removeStored(key: string): void {
  try {
    globalThis.localStorage?.removeItem(key);
  } catch {
    // Ignored.
  }
}

/** Keys the UI stores per viewer. */
export const STORAGE_KEYS = {
  language: "devbox.language",
  dockCollapsed: "devbox.dock.collapsed",
  /** Written by ZTheme (the key of provideZenitTheme below). */
  theme: "devbox.theme",
} as const;
