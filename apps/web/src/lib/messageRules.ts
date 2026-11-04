import type { Key } from "../i18n/en";
import { t } from "./i18n.svelte";

// Message rules (Server settings → Message rules, admins): blocked words
// and link domains, limits for new accounts and guests, and word
// replacements. The server checks every message (staff are exempt, but
// not from replacements) and says why it refuses one; only admins get the
// rules.
export type Replacement = { phrase: string; with: string[] }; // one of with, at random
export type MessageRules = {
  newMinutes: number; // accounts younger than this, and guests, are limited (0: off)
  words: string[]; // words and phrases, matched as whole words in any case
  domains: string[]; // with their subdomains
  replacements: Replacement[];
};

export const MAX_NEW_MINUTES = 7 * 24 * 60;

const isStrings = (v: unknown): v is string[] => Array.isArray(v) && v.every((s) => typeof s === "string");

const isReplacements = (v: unknown): v is Replacement[] =>
  Array.isArray(v) && v.every((p) => p && typeof p === "object" && typeof p.phrase === "string" && isStrings(p.with));

export const parseMessageRules = (v: unknown): MessageRules | null => {
  if (!v || typeof v !== "object") return null;
  const r = v as Record<string, unknown>;
  const replacements = r.replacements ?? []; // none from an older server
  return typeof r.newMinutes === "number" && isStrings(r.words) && isStrings(r.domains) && isReplacements(replacements)
    ? { newMinutes: r.newMinutes, words: r.words, domains: r.domains, replacements }
    : null;
};

// The lists are edited one entry per line.
export const parseList = (text: string) =>
  text
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
export const formatList = (list: readonly string[]) => list.join("\n");

// Replacements are edited one phrase per line: "nice ass => beautiful
// behind | lovely posterior". The checks are the server's: a phrase is
// words (as blocked words), a replacement plain text that nothing formats,
// links or mentions (as wordEntry and plainText in server/internal/msgrules).
const MAX_ENTRY = 100;
const MAX_WITH = 20;
const PHRASE = /^[\p{L}\p{N}\p{M}]+(?:(?: |['’._-])[\p{L}\p{N}\p{M}]+)*$/u;
const W = "[\\p{L}\\p{N}\\p{M}\\p{So}\\u{1F3FB}-\\u{1F3FF}\\u200D\\uFE0F]";
const PLAIN = new RegExp(`^${W}+(?:[ '’.,!?-]+${W}+)*[.,!?]*$`, "u");
const tidy = (s: string) => s.trim().split(/\s+/).join(" ");
const tooLong = (s: string) => [...s].length > MAX_ENTRY;

// A problem: its text's key and values (the line number comes later).
type Problem = { problem: Key; max?: number; text?: string };

const replacementOf = (line: string): Replacement | Problem => {
  const at = line.indexOf("=>");
  if (at < 0) return { problem: "admin.rulesNeedArrow" };
  const phrase = tidy(line.slice(0, at));
  const withs = [...new Set(line.slice(at + 2).split("|").map(tidy).filter(Boolean))];
  if (!phrase) return { problem: "admin.rulesPhraseMissing" };
  if (tooLong(phrase) || !PHRASE.test(phrase)) return { problem: "admin.rulesPhraseInvalid", max: MAX_ENTRY };
  if (withs.length === 0 || withs.length > MAX_WITH) return { problem: "admin.rulesReplacementCount", max: MAX_WITH };
  const bad = withs.find((w) => tooLong(w) || !PLAIN.test(w));
  if (bad !== undefined) return { problem: "admin.rulesReplacementInvalid", max: MAX_ENTRY, text: bad };
  return { phrase, with: withs };
};

export const parseReplacements = (text: string) => {
  const replacements: Replacement[] = [];
  const errors: string[] = [];
  text.split("\n").forEach((line, i) => {
    if (!line.trim()) return;
    const r = replacementOf(line);
    if ("problem" in r) {
      const { problem, ...vars } = r;
      errors.push(t(problem, { ...vars, line: i + 1 }));
    } else replacements.push(r);
  });
  return { replacements, errors };
};
export const formatReplacements = (list: readonly Replacement[]) =>
  list.map((r) => `${r.phrase} => ${r.with.join(" | ")}`).join("\n");

export const minutesError = (text: string) => {
  const n = Number(text.trim());
  return text.trim() !== "" && Number.isInteger(n) && n >= 0 && n <= MAX_NEW_MINUTES
    ? null
    : t("admin.rulesMinutesError", { max: MAX_NEW_MINUTES });
};

// The form's fields as rules (the server checks and tidies them again).
export const rulesFrom = (minutes: string, words: string, domains: string, replacements: Replacement[]): MessageRules => ({
  newMinutes: Number(minutes.trim()),
  words: parseList(words),
  domains: parseList(domains),
  replacements,
});
