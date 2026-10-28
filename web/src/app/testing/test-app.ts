// Shared set-up of the Vitest suites: the mock backend instead of the core, and a fake
// terminal instead of xterm.js (which needs a real canvas).

import { provideHttpClient, withInterceptors } from "@angular/common/http";
import {
  provideZonelessChangeDetection,
  type EnvironmentProviders,
  type Provider,
} from "@angular/core";
import { TestBed, type ComponentFixture } from "@angular/core/testing";
import { provideRouter } from "@angular/router";
import { vi } from "vitest";
import { apiInterceptor } from "../api/interceptor";
import { MockBackend } from "../mock/mock-backend";
import { mockInterceptor } from "../mock/mock-interceptor";
import { mockSocketFactoryProvider } from "../mock/mock-socket";
import {
  TERMINAL_FACTORY,
  type TerminalColors,
  type TerminalFactory,
  type TerminalLike,
  type TerminalSize,
} from "../terminal/terminal";

/** A terminal that records what is written to it. */
export class FakeTerminal implements TerminalLike {
  cols = 100;
  rows = 30;
  text = "";
  resets = 0;
  colors: TerminalColors | null = null;
  private readonly dataCallbacks: ((data: string) => void)[] = [];
  private readonly resizeCallbacks: ((size: TerminalSize) => void)[] = [];

  write(data: Uint8Array | string): void {
    this.text += typeof data === "string" ? data : new TextDecoder().decode(data);
  }
  reset(): void {
    this.text = "";
    this.resets++;
  }
  fit(): void {
    // Nothing to measure in jsdom.
  }
  focus(): void {
    // No focus handling needed.
  }
  setColors(colors: TerminalColors): void {
    this.colors = colors;
  }
  onData(callback: (data: string) => void): void {
    this.dataCallbacks.push(callback);
  }
  onResize(callback: (size: TerminalSize) => void): void {
    this.resizeCallbacks.push(callback);
  }
  dispose(): void {
    this.dataCallbacks.length = 0;
  }
  /** Simulates the user typing. */
  type(text: string): void {
    for (const callback of this.dataCallbacks) {
      callback(text);
    }
  }
  resize(size: TerminalSize): void {
    this.cols = size.cols;
    this.rows = size.rows;
    for (const callback of this.resizeCallbacks) {
      callback(size);
    }
  }
}

export const fakeTerminals: FakeTerminal[] = [];

export const fakeTerminalFactory: TerminalFactory = () => {
  const terminal = new FakeTerminal();
  fakeTerminals.push(terminal);
  return Promise.resolve(terminal);
};

/** Providers for a test: mock transport, router without routes, fake terminal. */
type AnyProvider = Provider | EnvironmentProviders;

export function testProviders(extra: AnyProvider[] = []): AnyProvider[] {
  return [
    provideZonelessChangeDetection(),
    provideRouter([]),
    provideHttpClient(withInterceptors([apiInterceptor, mockInterceptor])),
    mockSocketFactoryProvider,
    { provide: TERMINAL_FACTORY, useValue: fakeTerminalFactory },
    ...extra,
  ];
}

/** Configures TestBed with the mock backend; the simulated sessions run in milliseconds. */
export function setUpApp(extra: AnyProvider[] = []): MockBackend {
  localStorage.clear();
  fakeTerminals.length = 0;
  TestBed.configureTestingModule({ providers: testProviders(extra) });
  const backend = TestBed.inject(MockBackend);
  backend.latency = 0;
  backend.stepMs = 5;
  return backend;
}

/** Lets pending work finish and renders again, until `check` holds (or the time is up). */
export async function settle(
  fixture: ComponentFixture<unknown>,
  check?: () => void,
): Promise<void> {
  await fixture.whenStable();
  fixture.detectChanges();
  if (check) {
    await vi.waitFor(
      async () => {
        await fixture.whenStable();
        fixture.detectChanges();
        check();
      },
      { timeout: 4000, interval: 10 },
    );
  }
}

/** A fixture, or any element inside it, to search in. */
export type Root = ComponentFixture<unknown> | Element;

function rootElement(root: Root): Element {
  return root instanceof Element ? root : (root.nativeElement as Element);
}

export function query<T extends Element = HTMLElement>(root: Root, selector: string): T | null {
  return rootElement(root).querySelector<T>(selector);
}

export function queryAll<T extends Element = HTMLElement>(root: Root, selector: string): T[] {
  return [...rootElement(root).querySelectorAll<T>(selector)];
}

/** The first element matching `selector` whose text contains `text`. */
export function byText<T extends HTMLElement = HTMLElement>(
  root: Root,
  selector: string,
  text: string,
): T | undefined {
  return queryAll<T>(root, selector).find((el) => (el.textContent ?? "").includes(text));
}

/** Types into an input and fires the events a browser would. */
export function typeInto(input: HTMLInputElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
}
