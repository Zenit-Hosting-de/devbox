import { InjectionToken } from "@angular/core";

export interface TerminalColors {
  background: string;
  foreground: string;
  cursor: string;
  selection: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightWhite: string;
}

export interface TerminalSize {
  cols: number;
  rows: number;
}

/** What the application needs from a terminal; xterm.js behind the real factory. */
export interface TerminalLike {
  readonly cols: number;
  readonly rows: number;
  write(data: Uint8Array | string): void;
  reset(): void;
  fit(): void;
  focus(): void;
  setColors(colors: TerminalColors): void;
  /** Enables the keyboard input and the blinking cursor, or turns both off. */
  setInteractive(interactive: boolean): void;
  onData(callback: (data: string) => void): void;
  onResize(callback: (size: TerminalSize) => void): void;
  dispose(): void;
}

export interface TerminalOptions {
  readOnly: boolean;
  colors: TerminalColors;
  fontFamily: string;
}

export type TerminalFactory = (
  host: HTMLElement,
  options: TerminalOptions,
) => Promise<TerminalLike>;

/** Terminal font size in CSS pixels (xterm needs a number). */
export const TERMINAL_FONT_SIZE = 13;

/** Reads a CSS custom property of the root element; empty when unset. */
function token(style: CSSStyleDeclaration, name: string): string {
  return style.getPropertyValue(name).trim();
}

/** The terminal palette, taken from the design tokens that are active right now. */
export function terminalColorsFromTokens(root: HTMLElement): TerminalColors {
  const style = getComputedStyle(root);
  const get = (name: string, fallback: string): string => token(style, name) || fallback;
  const text = get("--text", "currentColor");
  return {
    background: get("--bg", "transparent"),
    foreground: text,
    cursor: get("--accent-text", text),
    selection: get("--surface-hover", text),
    black: get("--surface-raised", text),
    red: get("--danger", text),
    green: get("--success", text),
    yellow: get("--warning", text),
    blue: get("--info", text),
    magenta: get("--accent-text", text),
    cyan: get("--chart-3", text),
    white: get("--text-muted", text),
    brightWhite: text,
  };
}

/** The real terminal: xterm.js and its add-ons are loaded on first use. */
export const xtermFactory: TerminalFactory = async (host, options) => {
  const [{ Terminal }, { FitAddon }, { WebLinksAddon }] = await Promise.all([
    import("@xterm/xterm"),
    import("@xterm/addon-fit"),
    import("@xterm/addon-web-links"),
  ]);
  const term = new Terminal({
    cursorBlink: !options.readOnly,
    disableStdin: options.readOnly,
    fontFamily: options.fontFamily,
    fontSize: TERMINAL_FONT_SIZE,
    scrollback: 5000,
    theme: options.colors,
    allowProposedApi: false,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.loadAddon(new WebLinksAddon());
  term.open(host);
  return {
    get cols() {
      return term.cols;
    },
    get rows() {
      return term.rows;
    },
    write: (data) => {
      term.write(data);
    },
    reset: () => {
      term.reset();
    },
    fit: () => {
      try {
        fit.fit();
      } catch {
        // The host is hidden or has no size yet.
      }
    },
    focus: () => {
      term.focus();
    },
    setColors: (colors) => {
      term.options.theme = colors;
    },
    setInteractive: (interactive) => {
      term.options.disableStdin = !interactive;
      term.options.cursorBlink = interactive;
    },
    onData: (callback) => {
      term.onData(callback);
    },
    onResize: (callback) => {
      term.onResize(callback);
    },
    dispose: () => {
      term.dispose();
    },
  };
};

export const TERMINAL_FACTORY = new InjectionToken<TerminalFactory>("TERMINAL_FACTORY", {
  providedIn: "root",
  factory: () => xtermFactory,
});
