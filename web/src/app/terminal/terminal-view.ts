import { DOCUMENT } from "@angular/common";
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from "@angular/core";
import { ZTheme } from "zenit-ui";
import type { SessionChannel } from "../ws/session-channel";
import {
  TERMINAL_FACTORY,
  terminalColorsFromTokens,
  type TerminalLike,
  type TerminalSize,
} from "./terminal";

/** Delay before a size change is reported, so dragging a window does not flood the socket. */
export const RESIZE_DEBOUNCE_MS = 150;

/**
 * A terminal (xterm.js, loaded on first use). It shows either the live data of a channel or a
 * fixed log, reports keyboard input and size changes, and follows the colour scheme.
 */
@Component({
  selector: "dbx-terminal",
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<div #host class="term" role="group" [attr.aria-label]="label()"></div>`,
  styles: `
    :host {
      display: block;
      min-height: 0;
      min-width: 0;
    }
    .term {
      block-size: 100%;
      padding: var(--space-2);
      background: var(--bg);
      border-radius: var(--radius-sm);
      overflow: hidden;
    }
  `,
})
export class TerminalView {
  /** Live data of a session. */
  readonly channel = input<SessionChannel | null>(null);
  /** A fixed log (history), written once. */
  readonly data = input<Uint8Array | null>(null);
  /** Keyboard input is sent only while true (the session runs). */
  readonly interactive = input(false);
  /** False while the tab is hidden; the terminal is fitted again when it becomes true. */
  readonly active = input(true);
  readonly label = input("");

  readonly typed = output<string>();
  readonly resized = output<TerminalSize>();

  private readonly host = viewChild.required<ElementRef<HTMLElement>>("host");
  private readonly factory = inject(TERMINAL_FACTORY);
  private readonly doc = inject(DOCUMENT);
  private readonly theme = inject(ZTheme);
  private readonly destroyRef = inject(DestroyRef);
  private readonly term = signal<TerminalLike | null>(null);
  private resizeTimer: ReturnType<typeof setTimeout> | undefined;
  private destroyed = false;

  constructor() {
    afterNextRender(() => {
      void this.init();
    });

    // Live data: attach to the channel; a reset request clears the terminal at once.
    effect((onCleanup) => {
      const term = this.term();
      const channel = this.channel();
      if (!term || !channel) {
        return;
      }
      channel.attach(
        (bytes) => {
          term.write(bytes);
        },
        () => {
          term.reset();
        },
      );
      onCleanup(() => {
        channel.detach();
      });
    });

    // Fixed log.
    effect(() => {
      const term = this.term();
      const data = this.data();
      if (term && data) {
        term.reset();
        term.write(data);
      }
    });

    // Colour scheme.
    effect(() => {
      this.theme.resolvedScheme();
      this.theme.accent();
      const term = this.term();
      if (term) {
        queueMicrotask(() => {
          term.setColors(terminalColorsFromTokens(this.doc.documentElement));
        });
      }
    });

    // Keyboard input follows the session: a terminal created before the session was known
    // to be live still becomes interactive later.
    effect(() => {
      const term = this.term();
      const interactive = this.interactive();
      term?.setInteractive(interactive);
    });

    // Tab became visible.
    effect(() => {
      const term = this.term();
      if (term && this.active()) {
        requestAnimationFrame(() => {
          term.fit();
        });
      }
    });

    this.destroyRef.onDestroy(() => {
      this.destroyed = true;
      clearTimeout(this.resizeTimer);
      this.term()?.dispose();
    });
  }

  focus(): void {
    this.term()?.focus();
  }

  private async init(): Promise<void> {
    const host = this.host().nativeElement;
    const root = this.doc.documentElement;
    const term = await this.factory(host, {
      readOnly: !this.interactive(),
      colors: terminalColorsFromTokens(root),
      fontFamily: getComputedStyle(root).getPropertyValue("--font-mono").trim() || "monospace",
    });
    if (this.destroyed) {
      term.dispose();
      return;
    }
    term.onData((text) => {
      if (this.interactive()) {
        this.typed.emit(text);
      }
    });
    term.onResize((size) => {
      clearTimeout(this.resizeTimer);
      this.resizeTimer = setTimeout(() => {
        this.resized.emit(size);
      }, RESIZE_DEBOUNCE_MS);
    });
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => {
            if (this.active()) {
              term.fit();
            }
          });
    observer?.observe(host);
    this.destroyRef.onDestroy(() => {
      observer?.disconnect();
    });
    this.term.set(term);
    term.fit();
    this.resized.emit({ cols: term.cols, rows: term.rows });
  }
}
