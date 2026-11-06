import { describe, expect, it } from "vitest";
import { detect, registryOf, route, targetOf } from "../src/lib/translate/languages";

describe("translation languages", () => {
  it("tells the language of a chat message by its script and common words", () => {
    expect(detect("意思是我不用把它翻译成英文再发是吗")).toBe("zh");
    expect(detect("這個問題我們還沒有討論過")).toBe("zh-Hant");
    expect(detect("これは日本語のメッセージです")).toBe("ja");
    expect(detect("안녕하세요 반갑습니다")).toBe("ko");
    expect(detect("Привет, как дела?")).toBe("ru");
    expect(detect("why do i need to learn chinese? the models will translate for me")).toBe("en");
    expect(detect("Ich weiß nicht, ob das so funktioniert, aber wir können es versuchen")).toBe("de");
    expect(detect("je ne sais pas si c'est une bonne idée pour le serveur")).toBe("fr");
    expect(detect("no sé si es una buena idea para el servidor")).toBe("es");
    // Links, mentions and emoji do not count; too little text: unknown.
    expect(detect("@lain https://github.com/kallisti-chat/kallisti/pull/18 :tada:")).toBeNull();
    expect(detect("ok")).toBeNull();
  });

  it("translates into the app's language", () => {
    expect(targetOf("zh-Hans")).toBe("zh");
    expect(targetOf("zh-Hant")).toBe("zh-Hant");
    expect(targetOf("de")).toBe("de");
    expect(targetOf("en")).toBe("en");
  });

  it("goes straight, or through English", () => {
    const pairs = new Set(["zh-en", "en-zh", "en-de", "de-en"]);
    expect(route("zh", "en", pairs)).toEqual([["zh", "en"]]);
    expect(route("zh", "de", pairs)).toEqual([["zh", "en"], ["en", "de"]]);
    expect(route("zh", "zh", pairs)).toEqual([]);
    expect(route("ja", "en", pairs)).toBeNull();
  });

  it("lists the server's models for the translator, at the server", () => {
    const models = { pairs: { "zh-en": { model: { sha256: "aa", size: 3 }, vocab: { sha256: "bb", size: 1 } } } };
    expect(registryOf(models, "https://chat.example")).toEqual([
      {
        from: "zh",
        to: "en",
        files: {
          model: { name: "https://chat.example/api/translation/files/aa", size: 3, expectedSha256Hash: "aa" },
          vocab: { name: "https://chat.example/api/translation/files/bb", size: 1, expectedSha256Hash: "bb" },
        },
      },
    ]);
  });
});
