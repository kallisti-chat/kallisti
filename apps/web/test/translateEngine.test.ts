import { afterEach, describe, expect, it, vi } from "vitest";
import { translate } from "../src/lib/translate/engine";

const fakeTranslator = (availability: string) => ({
  availability: vi.fn(async () => availability),
  create: vi.fn(async () => ({ translate: async (text: string) => `[en] ${text}` })),
});

describe("translation engine", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("uses the browser's translator when the pair is on the device", async () => {
    const api = fakeTranslator("available");
    vi.stubGlobal("Translator", api);
    expect(await translate("你好", "zh", "en", "https://a.example")).toBe("[en] 你好");
    expect(api.create).toHaveBeenCalledWith({ sourceLanguage: "zh", targetLanguage: "en" });
  });

  it("does not make the browser download a pair: the server's models then", async () => {
    const api = fakeTranslator("downloadable");
    vi.stubGlobal("Translator", api);
    const fetch = vi.fn(async () => new Response("", { status: 404 }));
    vi.stubGlobal("fetch", fetch);
    await expect(translate("你好", "zh", "de", "https://b.example")).rejects.toThrow("models: 404");
    expect(api.create).not.toHaveBeenCalled();
    expect(fetch).toHaveBeenCalledWith("https://b.example/api/translation/models", { credentials: "omit" });
  });
});
