import { en, type Key } from "../i18n/en";
import { parseLanguage, pickLocale, translate, type Language, type Locale, type Messages, type Vars } from "./i18n";
import { persisted } from "./persisted.svelte";

// The language setting (per device) and the texts of the language in use.
// Other languages than English load on demand (one chunk each).
const languageStore = persisted("language", parseLanguage);
const loaders = import.meta.glob<{ default: Messages }>(["../i18n/*/index.ts", "!../i18n/en/index.ts"]);

let locale = $state<Locale>("en");
let dict = $state.raw<Messages>({});

const browserLanguages = (): readonly string[] =>
  typeof navigator === "undefined" ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];

const load = async (next: Locale) => {
  dict = next === "en" ? {} : (await loaders[`../i18n/${next}/index.ts`]!()).default;
  locale = next;
  if (typeof document !== "undefined") document.documentElement.lang = next;
};

export const i18n = {
  // The language in use, also for dates and numbers (Intl).
  get locale() {
    return locale;
  },
  get language() {
    return languageStore.value;
  },
  setLanguage: (language: Language) => {
    languageStore.set(language);
    return load(pickLocale(language, browserLanguages()));
  },
  // Before the app shows: its first texts are in the right language.
  init: () => load(pickLocale(languageStore.value, browserLanguages())).catch((err) => console.error("language not loaded", err)),
};

// A text in the language in use (English if it has none).
export const t = (key: Key, vars?: Vars): string => translate([[locale, dict], ["en", en]], key, vars);
