// The client's languages (see src/i18n: English is the source and the key
// type; the others may leave keys out, which then show in English).

export const LOCALES = ["en", "de", "fr", "es", "ja", "zh-Hans", "zh-Hant"] as const;
export type Locale = (typeof LOCALES)[number];
// The setting: a language, or the browser's ("auto").
export type Language = Locale | "auto";

// Each language in itself, for the language choice.
export const LOCALE_NAMES: Record<Locale, string> = {
  en: "English",
  de: "Deutsch",
  fr: "Français",
  es: "Español",
  ja: "日本語",
  "zh-Hans": "简体中文",
  "zh-Hant": "繁體中文",
};

// A text, or its plural forms (Intl.PluralRules categories, by "count").
export type Message = string | ({ other: string } & Partial<Record<Intl.LDMLPluralRule, string>>);
export type Messages = Readonly<Record<string, Message>>;
export type Vars = Readonly<Record<string, string | number>>;

const isLocale = (v: unknown): v is Locale => LOCALES.includes(v as Locale);

// A browser language tag as one of ours (null: none). Chinese by script:
// Traditional in Taiwan, Hong Kong and Macau.
const localeOf = (tag: string): Locale | null => {
  const [lang, ...rest] = tag.toLowerCase().split("-");
  if (lang === "zh") return rest.some((p) => ["hant", "tw", "hk", "mo"].includes(p)) ? "zh-Hant" : "zh-Hans";
  return LOCALES.find((l) => l === lang) ?? null;
};

// The saved choice, else the first browser language we have, else English.
export const pickLocale = (saved: string | null, languages: readonly string[]): Locale =>
  isLocale(saved) ? saved : (languages.map(localeOf).find((l) => l !== null) ?? "en");

// The tag for dates and numbers (Intl): the browser's own tag when it is
// the language in use (en-GB), else ours. With "auto" and none of our
// languages in the browser, the browser's: English texts, its formats.
export const formatLocale = (locale: Locale, language: Language, languages: readonly string[]): string =>
  languages.find((tag) => localeOf(tag) === locale) ??
  (language === "auto" && languages.length > 0 && languages.every((tag) => localeOf(tag) === null) ? languages[0]! : locale);

export const parseLanguage = (raw: string | null): Language => {
  try {
    const v: unknown = JSON.parse(raw ?? "null");
    return isLocale(v) ? v : "auto";
  } catch {
    return "auto";
  }
};

const fill = (text: string, vars: Vars | undefined) =>
  vars ? text.replace(/\{(\w+)\}/g, (all, name: string) => (name in vars ? String(vars[name]) : all)) : text;

// key's text from the first dictionary that has it (each with its
// language, for the plural rules), with the values filled in; the key
// itself if none has it.
export const translate = (dicts: readonly (readonly [Locale, Messages])[], key: string, vars?: Vars): string => {
  for (const [locale, dict] of dicts) {
    const m = dict[key];
    if (m === undefined) continue;
    if (typeof m === "string") return fill(m, vars);
    const count = Number(vars?.count ?? 0);
    return fill(m[new Intl.PluralRules(locale).select(count)] ?? m.other, vars);
  }
  return key;
};

// A sentence with an element (a link, a button) at {name}: the text before
// and after it, so the sentence stays whole for translators.
export const around = (text: string, name: string): [string, string] => {
  const i = text.indexOf(`{${name}}`);
  return i < 0 ? [text, ""] : [text.slice(0, i), text.slice(i + name.length + 2)];
};
