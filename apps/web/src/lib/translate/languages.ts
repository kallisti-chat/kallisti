import type { Locale } from "../i18n";

// Languages for translating messages (see lib/translate): codes as the
// server's model pairs name them ("zh-en", "en-zh-Hant").

// Characters only Traditional (or only Simplified) Chinese writes so.
const TRADITIONAL = "們個這來時說對會為學國後還過點開關體裡與無麼爲從難見車東電話問題討論經";
const SIMPLIFIED = "们个这来时说对会为学国后还过点开关体里与无么从难见车东电话问题讨论经";

// Common words of languages in the Latin script (none of one letter: "a"
// is English too).
const WORDS: Record<string, string[]> = {
  en: ["the", "and", "is", "are", "you", "to", "of", "it", "that", "do", "for", "this", "what", "not", "have", "with", "will", "me", "my"],
  de: ["der", "die", "das", "und", "ist", "nicht", "ich", "du", "wir", "es", "ein", "eine", "zu", "mit", "auf", "aber", "ob", "so", "können", "weiß"],
  fr: ["le", "la", "les", "et", "est", "je", "tu", "ne", "pas", "un", "une", "pour", "que", "c'est", "des", "du", "si", "sais", "nous", "vous"],
  es: ["el", "la", "los", "las", "es", "no", "que", "un", "una", "para", "por", "sé", "si", "de", "con", "pero", "muy", "yo", "está"],
  it: ["il", "la", "non", "che", "un", "una", "per", "di", "sono", "ma", "io", "questo", "come"],
  pt: ["os", "não", "que", "um", "uma", "para", "de", "eu", "você", "com", "mas"],
  nl: ["de", "het", "een", "en", "is", "niet", "ik", "je", "we", "dat", "van", "voor", "met", "maar", "ook"],
};

const count = (text: string, re: RegExp) => text.match(re)?.length ?? 0;

// The language of a chat message, or null when it cannot tell (too little
// text, words of no language here). Links, mentions and :emoji: do not
// count.
export const detect = (raw: string): string | null => {
  const text = raw.replace(/https?:\/\/\S+|@\S+|:[\w+-]+:|`[^`]*`/g, " ");
  const kana = count(text, /[\p{Script=Hiragana}\p{Script=Katakana}]/gu);
  const han = count(text, /\p{Script=Han}/gu);
  const scripts: [string, number][] = [
    ["ko", count(text, /\p{Script=Hangul}/gu)],
    ["ru", count(text, /\p{Script=Cyrillic}/gu)],
    ["ar", count(text, /\p{Script=Arabic}/gu)],
    ["el", count(text, /\p{Script=Greek}/gu)],
    ["he", count(text, /\p{Script=Hebrew}/gu)],
    ["th", count(text, /\p{Script=Thai}/gu)],
  ];
  // A script counts when it has a good part of the letters (a name or a
  // kaomoji in an English line does not).
  const letters = count(text, /\p{L}/gu);
  const most = (n: number) => n >= 2 && n >= letters * 0.3;
  if (most(kana + han) && kana >= 2) return "ja";
  if (most(han)) {
    const trad = [...text].filter((c) => TRADITIONAL.includes(c)).length;
    const simp = [...text].filter((c) => SIMPLIFIED.includes(c)).length;
    return trad > simp ? "zh-Hant" : "zh";
  }
  const [other, n] = scripts.reduce((a, b) => (b[1] > a[1] ? b : a));
  if (most(n)) return other === "ru" && /[іїєґ]/i.test(text) ? "uk" : other === "ar" && /[پچژگ]/.test(text) ? "fa" : other;
  const words = text.toLowerCase().match(/[\p{L}']+/gu) ?? [];
  const scores = Object.entries(WORDS).map(([lang, list]): [string, number] => [lang, words.filter((w) => list.includes(w)).length]);
  scores.sort((a, b) => b[1] - a[1]);
  const [best, second] = scores;
  return best![1] >= 2 && best![1] > second![1] ? best![0] : null;
};

// The language translations go into: the app's.
export const targetOf = (locale: Locale): string => (locale === "zh-Hans" ? "zh" : locale);

// The models go from one language to another: straight, or through
// English (Bergamot finds the way).
export const reachable = (from: string, to: string, pairs: ReadonlySet<string>) =>
  pairs.has(`${from}-${to}`) || (pairs.has(`${from}-en`) && pairs.has(`en-${to}`));

export type Models = { pairs: Record<string, Record<string, { sha256: string; size: number }>> };

// A pair's two languages ("zh-Hant-en": zh-Hant and en).
const sides = (pair: string): [string, string] => (pair.startsWith("en-") ? ["en", pair.slice(3)] : [pair.slice(0, -3), "en"]);

// The server's models as Bergamot's registry, with their addresses there.
export const registryOf = (models: Models, base: string) =>
  Object.entries(models.pairs).map(([pair, parts]) => {
    const [from, to] = sides(pair);
    const files = Object.fromEntries(
      Object.entries(parts).map(([part, f]) => [part, { name: `${base}/api/translation/files/${f.sha256}`, size: f.size, expectedSha256Hash: f.sha256 }]),
    );
    return { from, to, files };
  });
