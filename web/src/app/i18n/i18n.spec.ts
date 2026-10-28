import { TestBed } from "@angular/core/testing";
import { beforeEach, describe, expect, it } from "vitest";
import de from "../../i18n/de.json";
import en from "../../i18n/en.json";
import { PluginMessagesStore } from "./plugin-messages";
import { PluginText } from "./plugin-text";
import { formatMessage } from "./icu";
import { I18n, resolveMessage } from "./i18n";
import { PluginsStore } from "../stores/plugins.store";
import { setUpApp } from "../testing/test-app";

describe("formatMessage", () => {
  it("replaces {name} placeholders and keeps unknown ones", () => {
    expect(formatMessage("Hello {name}, {missing}", { name: "World" })).toBe(
      "Hello World, {missing}",
    );
  });

  it("selects the plural branch with Intl.PluralRules", () => {
    const message = "{count, plural, one {# change} other {# changes}}";
    expect(formatMessage(message, { count: 1 }, "en")).toBe("1 change");
    expect(formatMessage(message, { count: 5 }, "en")).toBe("5 changes");
    expect(formatMessage(message, { count: 0 }, "en")).toBe("0 changes");
  });

  it("supports exact matches and plurals inside text", () => {
    const message = "Found {count, plural, =0 {nothing} one {# plugin} other {# plugins}} here";
    expect(formatMessage(message, { count: 0 }, "en")).toBe("Found nothing here");
    expect(formatMessage(message, { count: 1 }, "en")).toBe("Found 1 plugin here");
    expect(formatMessage(message, { count: 1234 }, "en")).toBe("Found 1,234 plugins here");
  });

  it("uses the rules of the language", () => {
    const message = "{count, plural, one {# Änderung} other {# Änderungen}}";
    expect(formatMessage(message, { count: 1 }, "de")).toBe("1 Änderung");
    expect(formatMessage(message, { count: 2 }, "de")).toBe("2 Änderungen");
  });
});

describe("I18n", () => {
  let i18n: I18n;

  beforeEach(() => {
    setUpApp();
    i18n = TestBed.inject(I18n);
  });

  it("returns the text of the current language", async () => {
    expect(i18n.language()).toBe("en");
    expect(i18n.t("nav.overview")).toBe("Overview");
    await i18n.setLanguage("de");
    expect(i18n.t("nav.overview")).toBe("Übersicht");
  });

  it("switches without a reload, updates <html lang> and remembers the choice", async () => {
    await i18n.setLanguage("de");
    TestBed.tick();
    expect(document.documentElement.lang).toBe("de");
    expect(localStorage.getItem("devbox.language")).toBe("de");
  });

  it("falls back to English and then to the key itself, never to empty text", () => {
    expect(i18n.t("no.such.key")).toBe("no.such.key");
    expect(i18n.t("no.such.key")).not.toBe("");
  });

  it("looks up the language first, then English, then nothing", () => {
    const bundles = {
      en: { "a.both": "English both", "a.englishOnly": "English only" },
      de: { "a.both": "Deutsch beide", "a.germanOnly": "Nur Deutsch" },
    };
    expect(resolveMessage(bundles, "de", "a.both")).toBe("Deutsch beide");
    expect(resolveMessage(bundles, "de", "a.englishOnly")).toBe("English only");
    expect(resolveMessage(bundles, "de", "a.germanOnly")).toBe("Nur Deutsch");
    expect(resolveMessage(bundles, "en", "a.germanOnly")).toBeUndefined();
    expect(resolveMessage({}, "de", "nav.overview")).toBe("Overview");
  });

  it("formats parameters and plurals", () => {
    expect(i18n.t("settings.changes", { count: 1 })).toBe("1 change");
    expect(i18n.t("settings.changes", { count: 3 })).toBe("3 changes");
    expect(i18n.t("history.steps")).toBe("Steps");
  });

  it("takes the configured language unless the viewer chose one", async () => {
    await i18n.applyConfigured("de-DE");
    expect(i18n.language()).toBe("de");
  });

  it("ignores the configured language after the viewer chose one", async () => {
    await i18n.setLanguage("en");
    await i18n.applyConfigured("de");
    expect(i18n.language()).toBe("en");
  });

  it("has the same keys in German and English and no empty text", () => {
    const keys = (file: Record<string, unknown>): string[] =>
      Object.keys(file)
        .filter((k) => !k.startsWith("$"))
        .sort();
    expect(keys(de)).toEqual(keys(en));
    for (const file of [de, en] as Record<string, unknown>[]) {
      for (const [key, value] of Object.entries(file)) {
        if (!key.startsWith("$")) {
          expect(value, key).toBeTypeOf("string");
          expect((value as string).trim(), key).not.toBe("");
        }
      }
    }
  });

  it("keeps the texts neutral: no du, Sie or ich forms", () => {
    const forbidden = /\b(du|dich|dir|dein\w*|sie|ihr\w*|ich|mich|mir|mein\w*)\b/i;
    for (const [key, value] of Object.entries(de)) {
      if (!key.startsWith("$")) {
        expect(forbidden.test(value as string), `${key}: ${value as string}`).toBe(false);
      }
    }
  });
});

describe("plugin texts", () => {
  beforeEach(() => {
    setUpApp();
  });

  it("prefers the plugin translation for the language, then the manifest text", async () => {
    const i18n = TestBed.inject(I18n);
    const plugins = TestBed.inject(PluginsStore);
    const messages = TestBed.inject(PluginMessagesStore);
    const text = TestBed.inject(PluginText);
    await plugins.reload();
    await i18n.setLanguage("de");
    await messages.ensure(plugins.plugins(), "de");

    // java/de has the key; the manifest says "Install JDK".
    expect(text.actionLabel("java", "install", "Install JDK")).toBe("JDK installieren");
    // java/de has no translation for this key: the manifest text is shown.
    expect(text.text("java", "settings.unknown.label", "Manifest text")).toBe("Manifest text");
    // A plugin without translation file (docker/de answers 404) shows the manifest text.
    expect(text.actionLabel("docker", "install", "Install Docker")).toBe("Install Docker");
  });

  it("shows the manifest text in a language without a translation file", async () => {
    const i18n = TestBed.inject(I18n);
    const plugins = TestBed.inject(PluginsStore);
    const messages = TestBed.inject(PluginMessagesStore);
    const text = TestBed.inject(PluginText);
    await plugins.reload();
    await messages.ensure(plugins.plugins(), "en");
    expect(i18n.language()).toBe("en");
    expect(text.actionLabel("java", "install", "Install JDK")).toBe("Install JDK");
  });

  it("translates category labels and shows unknown categories as written", () => {
    const text = TestBed.inject(PluginText);
    expect(text.category("runtime")).toBe("Runtimes");
    expect(text.category("something-new")).toBe("something-new");
  });
});
