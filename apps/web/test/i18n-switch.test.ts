// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { i18n, t } from "../src/lib/i18n.svelte";

describe("switching the language", () => {
  it("loads the language, keeps the choice and marks the page", async () => {
    expect(t("settings.language")).toBe("Language");
    await i18n.setLanguage("de");
    expect(t("settings.language")).toBe("Sprache");
    expect(i18n.locale).toBe("de"); // jsdom: en-US, no German region
    expect(document.documentElement.lang).toBe("de");
    expect(localStorage.getItem("language")).toBe('"de"');
    await i18n.setLanguage("auto"); // jsdom's browser language: en-US
    expect(t("settings.language")).toBe("Language");
  });

  it("keeps the latest choice when an earlier one loads later", async () => {
    const slow = i18n.setLanguage("ja");
    await i18n.setLanguage("en");
    await slow;
    expect(t("settings.language")).toBe("Language");
    expect(document.documentElement.lang).toBe("en");
  });
});
