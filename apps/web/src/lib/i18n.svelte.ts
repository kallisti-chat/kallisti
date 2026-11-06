import { en, type Key } from "../i18n/en";
import { formatLocale, parseLanguage, pickLocale, translate, type Language, type Locale, type Messages, type Vars } from "./i18n";
import { persisted } from "./persisted.svelte";

// The language setting (per device) and the texts of the language in use.
// Other languages than English load on demand (one chunk each).
const loaders = import.meta.glob<{ default: Messages }>(["../i18n/*/index.ts", "!../i18n/en/index.ts"]);

// Made on first use: modules that only translate (and their tests) need
// no storage.
let store: ReturnType<typeof persisted<Language>> | undefined;
const languageStore = () => (store ??= persisted("language", parseLanguage));

let locale = $state<Locale>("en");
let dict = $state.raw<Messages>({});
let loads = 0; // the latest choice wins over a slower earlier load

const browserLanguages = (): readonly string[] =>
  typeof navigator === "undefined" ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];

const load = async (next: Locale) => {
  const n = ++loads;
  const texts = next === "en" ? {} : (await loaders[`../i18n/${next}/index.ts`]!()).default;
  if (n !== loads) return;
  dict = texts;
  locale = next;
  if (typeof document !== "undefined") document.documentElement.lang = next;
};

const loadChosen = () =>
  load(pickLocale(languageStore().value, browserLanguages())).catch((err) => console.error("language not loaded", err));

export const i18n = {
  // The tag for dates and numbers (Intl), see formatLocale.
  get locale() {
    return formatLocale(locale, store?.value ?? "auto", browserLanguages());
  },
  get language() {
    return languageStore().value;
  },
  // The language of the texts in use (one of LOCALES).
  get ui() {
    return locale;
  },
  setLanguage: (language: Language) => {
    languageStore().set(language);
    return loadChosen();
  },
  // Before the app shows: its first texts are in the right language.
  init: loadChosen,
};

// A text in the language in use (English if it has none).
export const t = (key: Key, vars?: Vars): string => translate([[locale, dict], ["en", en]], key, vars);
