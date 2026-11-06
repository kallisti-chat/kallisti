import { describe, expect, it } from "vitest";
import { glossary, promptFor } from "../src/lib/translate/prompt";

describe("the prompt for the translation model", () => {
  it("has the app's words that are in the message, in the target language", () => {
    expect(glossary("Kannst du mal kurz in den Sprachkanal kommen?", "de", "en", [])).toEqual(["Sprachkanal translates to voice channel"]);
    expect(glossary("Franchement l'appli plante tout le temps sur mon tel.", "fr", "en", [])).toEqual([
      "plante translates to crash",
      "tel translates to phone",
    ]);
    expect(glossary("文字频道大家也没那么刚需实时翻译", "zh", "de", [])).toEqual(["文字频道 translates to Textkanal"]);
    // Whole words only in Latin scripts: not "tel" in "hôtel".
    expect(glossary("On dort à l'hôtel ce soir", "fr", "en", [])).toEqual([]);
  });

  it("keeps names as they are", () => {
    expect(glossary("@merrenlendell kommst du in #general?", "de", "en", ["merrenlendell", "lain", "general"])).toEqual([
      "merrenlendell translates to merrenlendell",
      "general translates to general",
    ]);
  });

  it("is Tencent's background prompt with the app, the messages before and the glossary", () => {
    const p = promptFor("Franchement l'appli plante", "fr", "en", { before: [{ author: "lain", text: "x".repeat(300) }], names: [] });
    expect(p).toContain("[Background Information]\nKallisti is a voice and text chat app");
    expect(p).toContain(`The messages before it in the chat:\nlain: ${"x".repeat(200)}…`);
    expect(p).toContain("Reference translations: plante translates to crash");
    expect(p).toContain("Please translate the following text into English, taking the provided background information into consideration.\n\n[Source Text]\nFranchement l'appli plante");
    expect(promptFor("你好", "zh", "zh-Hant", { before: [], names: [] })).toContain("into Traditional Chinese");
    expect(promptFor("hi there", "en", "zh", { before: [], names: [] })).not.toContain("messages before");
  });
});
