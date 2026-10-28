import { DOCUMENT } from "@angular/common";
import { Injectable, computed, effect, inject, signal } from "@angular/core";
import en from "../../i18n/en.json";
import { STORAGE_KEYS, readStored, writeStored } from "../core/storage";
import { formatMessage, type MessageParams } from "./icu";

export type Language = "de" | "en";

/** Selectable languages; the names are written in their own language and never translated. */
export const LANGUAGES: readonly { readonly code: Language; readonly name: string }[] = [
  { code: "de", name: "Deutsch" },
  { code: "en", name: "English" },
];

export const FALLBACK_LANGUAGE: Language = "en";

type Bundle = Readonly<Record<string, string>>;

/** Drops $schema and $comment; what is left are the messages. */
export function messagesOf(file: Readonly<Record<string, unknown>>): Bundle {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(file)) {
    if (!key.startsWith("$") && typeof value === "string" && value !== "") {
      out[key] = value;
    }
  }
  return out;
}

/** English is the last fallback and always present; other languages load on demand. */
const ENGLISH: Bundle = messagesOf(en);

const LOADERS: Readonly<Record<Language, () => Promise<Bundle>>> = {
  en: () => Promise.resolve(ENGLISH),
  de: () => import("../../i18n/de.json").then((m) => messagesOf(m.default)),
};

/**
 * The message for a key: the language, then English, then undefined (the caller shows the key,
 * so a text is never empty).
 */
export function resolveMessage(
  bundles: Readonly<Partial<Record<Language, Bundle>>>,
  language: Language,
  key: string,
): string | undefined {
  return bundles[language]?.[key] ?? bundles[FALLBACK_LANGUAGE]?.[key] ?? ENGLISH[key];
}

export function isLanguage(value: unknown): value is Language {
  return LANGUAGES.some((l) => l.code === value);
}

/** Maps a BCP 47 tag such as de-DE to a supported language. */
export function languageFromTag(tag: string | undefined | null): Language | undefined {
  const base = tag?.toLowerCase().split("-")[0];
  return LANGUAGES.find((l) => l.code === base)?.code;
}

/**
 * Core texts. Lookup order: current language, then English, then the key itself, so a text is
 * never empty. Switching the language only changes a signal; nothing reloads.
 */
@Injectable({ providedIn: "root" })
export class I18n {
  private readonly doc = inject(DOCUMENT);

  readonly language = signal<Language>(this.initialLanguage());
  readonly locale = computed(() => this.language());

  private readonly bundles = signal<Partial<Record<Language, Bundle>>>({ en: ENGLISH });

  /** True once the viewer picked a language (stored in the browser). */
  private chosen = readStored(STORAGE_KEYS.language) !== null;

  constructor() {
    effect(() => {
      this.doc.documentElement.lang = this.language();
    });
  }

  /** t('key', { name }) – an arrow property, so it can be handed to templates as is. */
  readonly t = (key: string, params?: MessageParams): string => {
    const lang = this.language();
    const raw = resolveMessage(this.bundles(), lang, key);
    if (raw === undefined) {
      return key;
    }
    return formatMessage(raw, params, lang);
  };

  has(key: string): boolean {
    return resolveMessage(this.bundles(), this.language(), key) !== undefined;
  }

  /** Loads the texts of a language (once). */
  async ensureLoaded(language: Language): Promise<void> {
    if (this.bundles()[language]) {
      return;
    }
    const bundle = await LOADERS[language]();
    this.bundles.update((all) => ({ ...all, [language]: bundle }));
  }

  /** Switches the language for this viewer and remembers it. No reload; texts update at once. */
  async setLanguage(language: Language): Promise<void> {
    this.chosen = true;
    writeStored(STORAGE_KEYS.language, language);
    await this.ensureLoaded(language);
    this.language.set(language);
  }

  /** Applies the language of the configuration, unless the viewer already chose one. */
  async applyConfigured(language: string | undefined): Promise<void> {
    const configured = languageFromTag(language);
    if (configured && !this.chosen) {
      await this.ensureLoaded(configured);
      this.language.set(configured);
    }
  }

  date(iso: string | undefined, style: "short" | "medium" = "medium"): string {
    if (!iso) {
      return "";
    }
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) {
      return iso;
    }
    return new Intl.DateTimeFormat(this.locale(), { dateStyle: style, timeStyle: "short" }).format(
      date,
    );
  }

  number(value: number): string {
    return new Intl.NumberFormat(this.locale()).format(value);
  }

  private initialLanguage(): Language {
    const stored = readStored(STORAGE_KEYS.language);
    if (isLanguage(stored)) {
      return stored;
    }
    const nav = globalThis.navigator;
    for (const tag of nav?.languages ?? [nav?.language]) {
      const found = languageFromTag(tag);
      if (found) {
        return found;
      }
    }
    return FALLBACK_LANGUAGE;
  }
}
