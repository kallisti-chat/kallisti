import { describe, expect, it } from "vitest";
import { en } from "../src/i18n/en";
import de from "../src/i18n/de";
import es from "../src/i18n/es";
import fr from "../src/i18n/fr";
import ja from "../src/i18n/ja";
import zhHans from "../src/i18n/zh-Hans";
import zhHant from "../src/i18n/zh-Hant";
import type { Message, Messages } from "../src/lib/i18n";

const forms = (m: Message) => (typeof m === "string" ? [m] : Object.values(m));
const placeholders = (m: Message) => [...new Set(forms(m).flatMap((f) => f.match(/\{\w+\}/g) ?? []))].sort();

describe.each([
  ["de", de],
  ["fr", fr],
  ["es", es],
  ["ja", ja],
  ["zh-Hans", zhHans],
  ["zh-Hant", zhHant],
] as [string, Messages][])("%s", (_, dict) => {
  it("has every text, with the same placeholders", () => {
    const missing = Object.keys(en).filter((k) => !(k in dict));
    expect(missing).toEqual([]);
    const wrong = Object.entries(en).filter(([k, m]) => k in dict && placeholders(dict[k]!).join() !== placeholders(m).join());
    expect(wrong.map(([k]) => k)).toEqual([]);
  });

  it("gives plurals as plurals, each with other", () => {
    const bad = Object.entries(en).filter(([k, m]) => {
      const d = dict[k];
      return d !== undefined && (typeof m === "string" ? typeof d !== "string" : typeof d === "string" || !("other" in d));
    });
    expect(bad.map(([k]) => k)).toEqual([]);
  });
});
