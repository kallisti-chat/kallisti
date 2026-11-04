import { describe, expect, it } from "vitest";
import { around, formatLocale, parseLanguage, pickLocale, translate } from "../src/lib/i18n";

describe("pickLocale", () => {
  it("takes the saved choice, else the first browser language we have", () => {
    expect(pickLocale("de", ["fr-FR"])).toBe("de");
    expect(pickLocale("auto", ["it-IT", "fr-CA", "en"])).toBe("fr");
    expect(pickLocale(null, ["it"])).toBe("en");
    expect(pickLocale("auto", [])).toBe("en");
  });

  it("tells the two Chinese scripts apart", () => {
    for (const tag of ["zh", "zh-CN", "zh-SG", "zh-Hans", "zh-Hans-CN"]) expect(pickLocale(null, [tag])).toBe("zh-Hans");
    for (const tag of ["zh-TW", "zh-HK", "zh-MO", "zh-Hant", "zh-Hant-HK"]) expect(pickLocale(null, [tag])).toBe("zh-Hant");
  });
});

describe("parseLanguage", () => {
  it("keeps a known language or auto", () => {
    expect(parseLanguage('"ja"')).toBe("ja");
    expect(parseLanguage('"auto"')).toBe("auto");
    expect(parseLanguage('"xx"')).toBe("auto");
    expect(parseLanguage("broken")).toBe("auto");
    expect(parseLanguage(null)).toBe("auto");
  });
});

describe("translate", () => {
  const en = { hi: "Hello, {name}!", msgs: { one: "{count} message", other: "{count} messages" }, only: "English" };
  const de = { hi: "Hallo, {name}!", msgs: { one: "{count} Nachricht", other: "{count} Nachrichten" } };
  const ja = { msgs: { other: "{count} 件のメッセージ" } };

  it("fills in values and falls back to English per key", () => {
    expect(translate([["de", de], ["en", en]], "hi", { name: "Ana" })).toBe("Hallo, Ana!");
    expect(translate([["de", de], ["en", en]], "only")).toBe("English");
    expect(translate([["de", de], ["en", en]], "hi")).toBe("Hallo, {name}!");
    expect(translate([["de", de], ["en", en]], "missing")).toBe("missing");
  });

  it("picks the plural form of the language", () => {
    expect(translate([["de", de], ["en", en]], "msgs", { count: 1 })).toBe("1 Nachricht");
    expect(translate([["de", de], ["en", en]], "msgs", { count: 3 })).toBe("3 Nachrichten");
    expect(translate([["ja", ja], ["en", en]], "msgs", { count: 1 })).toBe("1 件のメッセージ");
    expect(translate([["fr", {}], ["en", en]], "msgs", { count: 0 })).toBe("0 messages"); // English form, its rules
  });
});

describe("around", () => {
  it("splits a sentence at a placeholder, for an element there", () => {
    expect(around("Read the {link} first.", "link")).toEqual(["Read the ", " first."]);
    expect(around("{link} zuerst lesen.", "link")).toEqual(["", " zuerst lesen."]);
    expect(around("No placeholder", "link")).toEqual(["No placeholder", ""]);
  });
});

describe("formatLocale", () => {
  it("takes the browser's region for the language in use", () => {
    expect(formatLocale("en", "auto", ["en-GB", "de"])).toBe("en-GB");
    expect(formatLocale("de", "de", ["en-GB", "de-AT"])).toBe("de-AT");
    expect(formatLocale("de", "de", ["en-GB"])).toBe("de");
    expect(formatLocale("zh-Hant", "auto", ["zh-TW"])).toBe("zh-TW");
    expect(formatLocale("zh-Hans", "zh-Hans", ["zh-TW"])).toBe("zh-Hans");
  });

  it("keeps the browser's formats when it has none of our languages", () => {
    expect(formatLocale("en", "auto", ["nl-NL", "it"])).toBe("nl-NL");
    expect(formatLocale("en", "en", ["nl-NL"])).toBe("en");
  });
});
