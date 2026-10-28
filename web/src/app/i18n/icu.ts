// A small subset of ICU message format: {name} placeholders and
// {count, plural, one {...} other {...}} with # standing for the number.

export type MessageParams = Readonly<Record<string, string | number>>;

interface Block {
  /** Text between the braces, without the outer braces. */
  inner: string;
  /** Index after the closing brace. */
  end: number;
}

/** Reads a balanced {...} block starting at `start` (the index of the opening brace). */
function readBlock(text: string, start: number): Block | null {
  let depth = 0;
  for (let i = start; i < text.length; i++) {
    const ch = text[i];
    if (ch === "{") {
      depth++;
    } else if (ch === "}") {
      depth--;
      if (depth === 0) {
        return { inner: text.slice(start + 1, i), end: i + 1 };
      }
    }
  }
  return null;
}

function parsePluralBranches(source: string): Map<string, string> {
  const branches = new Map<string, string>();
  let i = 0;
  while (i < source.length) {
    while (i < source.length && /\s/.test(source[i] ?? "")) {
      i++;
    }
    const nameStart = i;
    while (i < source.length && source[i] !== "{" && !/\s/.test(source[i] ?? "")) {
      i++;
    }
    const name = source.slice(nameStart, i);
    while (i < source.length && /\s/.test(source[i] ?? "")) {
      i++;
    }
    if (source[i] !== "{") {
      break;
    }
    const block = readBlock(source, i);
    if (!block) {
      break;
    }
    branches.set(name, block.inner);
    i = block.end;
  }
  return branches;
}

function formatPlural(
  inner: string,
  params: MessageParams,
  locale: string,
  rules: Intl.PluralRules,
): string {
  const firstComma = inner.indexOf(",");
  const secondComma = inner.indexOf(",", firstComma + 1);
  const name = inner.slice(0, firstComma).trim();
  const branches = parsePluralBranches(inner.slice(secondComma + 1));
  const raw = params[name];
  const count = typeof raw === "number" ? raw : Number(raw);
  const exact = branches.get(`=${count}`);
  const category = rules.select(Number.isFinite(count) ? count : 0);
  const chosen = exact ?? branches.get(category) ?? branches.get("other") ?? "";
  const number = new Intl.NumberFormat(locale).format(Number.isFinite(count) ? count : 0);
  return render(chosen.replaceAll("#", number), params, locale, rules);
}

function render(
  text: string,
  params: MessageParams,
  locale: string,
  rules: Intl.PluralRules,
): string {
  let out = "";
  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (ch !== "{") {
      out += ch;
      i++;
      continue;
    }
    const block = readBlock(text, i);
    if (!block) {
      out += text.slice(i);
      break;
    }
    const inner = block.inner;
    if (inner.includes(",")) {
      const kind = inner.split(",")[1]?.trim();
      out += kind === "plural" ? formatPlural(inner, params, locale, rules) : `{${inner}}`;
    } else {
      const value = params[inner.trim()];
      out += value === undefined ? `{${inner}}` : String(value);
    }
    i = block.end;
  }
  return out;
}

/** Formats a message with its parameters. Unknown placeholders stay as written. */
export function formatMessage(message: string, params: MessageParams = {}, locale = "en"): string {
  if (!message.includes("{")) {
    return message;
  }
  let rules: Intl.PluralRules;
  try {
    rules = new Intl.PluralRules(locale);
  } catch {
    rules = new Intl.PluralRules("en");
  }
  return render(message, params, locale, rules);
}
