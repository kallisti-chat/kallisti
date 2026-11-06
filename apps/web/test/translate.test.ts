import { describe, expect, it } from "vitest";
import { detect, loadDetector, reachable, registryOf, targetOf } from "../src/lib/translate/languages";

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

  it("with the detector loaded, tells short Latin-script chat lines", async () => {
    await loadDetector();
    for (const [want, line] of [
      ["de", "Kannst du mal kurz in den Sprachkanal kommen?"],
      ["fr", "Ça marche nickel depuis la mise à jour, merci !"],
      ["fr", "On se retrouve dans le salon vocal dans 5 min ?"],
      ["fr", "Franchement l'appli plante tout le temps sur mon tel."],
      ["es", "Me uno al canal de voz en un rato, voy a cenar."],
      ["en", "fix a typo in a comment"],
      ["en", "translation test posts end here"],
    ] as const)
      expect([line, detect(line)]).toEqual([line, want]);
    // Still by script: Chinese, Simplified or Traditional; Japanese.
    expect(detect("這個功能什麼時候上線？我等好久了")).toBe("zh-Hant");
    expect(detect("それめっちゃいいね、助かる！")).toBe("ja");
  });

  it("does not take English with single letters, kaomoji or Chinese names for another language", () => {
    for (const line of ["fix a typo in a comment", "a bug in a test", "const a = b || a", "¯\\_(ツ)_/¯ no idea", "talked to 王伟 about it today, he said it was fine"])
      expect([line, detect(line)]).not.toEqual([line, expect.stringMatching(/^(pt|it|ja|zh)/)]);
    expect(detect("talked to 王伟 about it today, he said it was fine")).toBe("en");
  });

  it("translates into the app's language", () => {
    expect(targetOf("zh-Hans")).toBe("zh");
    expect(targetOf("zh-Hant")).toBe("zh-Hant");
    expect(targetOf("de")).toBe("de");
    expect(targetOf("en")).toBe("en");
  });

  it("goes straight, or through English", () => {
    const pairs = new Set(["zh-en", "en-zh", "en-de", "de-en"]);
    expect(reachable("zh", "en", pairs)).toBe(true);
    expect(reachable("zh", "de", pairs)).toBe(true);
    expect(reachable("ja", "en", pairs)).toBe(false);
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
