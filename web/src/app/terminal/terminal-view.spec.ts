import { Component, signal } from "@angular/core";
import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ZTheme } from "zenit-ui";
import { fakeTerminals, setUpApp, settle } from "../testing/test-app";
import { SessionChannel } from "../ws/session-channel";
import { bytesToBase64 } from "../ws/base64";
import type { TerminalSize } from "./terminal";
import { RESIZE_DEBOUNCE_MS, TerminalView } from "./terminal-view";

@Component({
  imports: [TerminalView],
  template: `
    <dbx-terminal
      [channel]="channel"
      [data]="data()"
      [interactive]="interactive()"
      (typed)="typed.push($event)"
      (resized)="sizes.push($event)"
    />
  `,
})
class Host {
  readonly channel = new SessionChannel("s_0123456789");
  readonly data = signal<Uint8Array | null>(null);
  readonly interactive = signal(true);
  readonly typed: string[] = [];
  readonly sizes: TerminalSize[] = [];
}

describe("TerminalView", () => {
  beforeEach(() => {
    setUpApp();
  });

  async function render() {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture, () => {
      expect(fakeTerminals).toHaveLength(1);
    });
    await settle(fixture);
    return fixture;
  }

  it("writes live channel data, including what arrived before the terminal existed", async () => {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.channel.write(new TextEncoder().encode("early "));
    await settle(fixture, () => {
      expect(fakeTerminals[0]?.text).toBe("early ");
    });
    fixture.componentInstance.channel.handle({
      type: "output",
      session: "s_0123456789",
      seq: 0,
      data: bytesToBase64(new TextEncoder().encode("late")),
    });
    expect(fakeTerminals[0]?.text).toBe("early late");
  });

  it("clears the terminal when the channel asks for a reset", async () => {
    const fixture = await render();
    fixture.componentInstance.channel.write(new TextEncoder().encode("old"));
    fixture.componentInstance.channel.resetTerminal();
    expect(fakeTerminals[0]?.resets).toBe(1);
    expect(fakeTerminals[0]?.text).toBe("");
  });

  it("shows a fixed log written once", async () => {
    const fixture = await render();
    fixture.componentInstance.data.set(new TextEncoder().encode("history"));
    await settle(fixture, () => {
      expect(fakeTerminals[0]?.text).toBe("history");
    });
  });

  it("reports keyboard input only while interactive", async () => {
    const fixture = await render();
    fakeTerminals[0]?.type("a");
    fixture.componentInstance.interactive.set(false);
    await settle(fixture);
    fakeTerminals[0]?.type("b");
    expect(fixture.componentInstance.typed).toEqual(["a"]);
  });

  it("switches the terminal input on when the session becomes live later", async () => {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.interactive.set(false);
    await settle(fixture, () => {
      expect(fakeTerminals).toHaveLength(1);
    });
    await settle(fixture, () => {
      expect(fakeTerminals[0]?.interactive).toBe(false);
    });
    fixture.componentInstance.interactive.set(true);
    await settle(fixture, () => {
      expect(fakeTerminals[0]?.interactive).toBe(true);
    });
    fakeTerminals[0]?.type("sudo");
    expect(fixture.componentInstance.typed).toEqual(["sudo"]);
  });

  it("reports size changes debounced", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const fixture = await render();
      fixture.componentInstance.sizes.length = 0;
      fakeTerminals[0]?.resize({ cols: 90, rows: 20 });
      fakeTerminals[0]?.resize({ cols: 91, rows: 21 });
      vi.advanceTimersByTime(RESIZE_DEBOUNCE_MS - 1);
      expect(fixture.componentInstance.sizes).toEqual([]);
      vi.advanceTimersByTime(1);
      expect(fixture.componentInstance.sizes).toEqual([{ cols: 91, rows: 21 }]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("takes its colours from the design tokens and follows the colour scheme", async () => {
    document.documentElement.style.setProperty("--bg", "rgb(1, 2, 3)");
    document.documentElement.style.setProperty("--text", "rgb(4, 5, 6)");
    try {
      const fixture = await render();
      await vi.waitFor(() => {
        expect(fakeTerminals[0]?.colors?.background).toBe("rgb(1, 2, 3)");
      });
      document.documentElement.style.setProperty("--bg", "rgb(9, 9, 9)");
      TestBed.inject(ZTheme).setScheme("light");
      await settle(fixture, () => {
        expect(fakeTerminals[0]?.colors?.background).toBe("rgb(9, 9, 9)");
      });
      expect(fakeTerminals[0]?.colors?.foreground).toBe("rgb(4, 5, 6)");
    } finally {
      document.documentElement.style.removeProperty("--bg");
      document.documentElement.style.removeProperty("--text");
    }
  });
});
