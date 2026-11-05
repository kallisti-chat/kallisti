import { describe, expect, it } from "vitest";
import { firstLink, previewKey, previewsToAsk } from "../src/lib/linkPreview";
import { initialTextState, parseServerEvent, reduceText, type TextMessage } from "../src/lib/textChannels";

const msg = (id: number, text: string): TextMessage => ({ id, channel: 1, author: "a", text, at: 1 });

describe("link previews", () => {
  it("finds the first link as the server does", () => {
    expect(firstLink("see https://x.org/a?b=1.")).toBe("https://x.org/a?b=1");
    expect(firstLink("<https://x.org/a> and http://y")).toBe("https://x.org/a");
    expect(firstLink("**https://x.org**")).toBe("https://x.org");
    expect(firstLink("javascript:alert(1)")).toBeNull();
    // Not in spoilers: the card would show what is hidden.
    expect(firstLink("||https://x.org|| and https://y.org")).toBe("https://y.org");
    expect(firstLink("||https://x.org||")).toBeNull();
    expect(firstLink("https://x.org||x")).toBe("https://x.org");
  });

  it("does not ask for links it skips (message links to this server)", () => {
    const msgs = [msg(1, "https://chat.example/channels/1/2"), msg(2, "https://a.org")];
    expect(previewsToAsk(msgs, {}, new Set(), (url) => url.startsWith("https://chat.example/"))).toEqual([msgs[1]]);
  });

  it("asks once per message and link, for messages with no answer yet", () => {
    const msgs = [msg(1, "https://a.org"), msg(2, "plain"), msg(3, "https://b.org"), msg(4, "https://c.org")];
    const asked = new Set([previewKey(msgs[3]!)]);
    expect(previewsToAsk(msgs, { 3: null }, asked)).toEqual([msgs[0]]);
    // Edited to another link: asked again.
    expect(previewsToAsk([msg(4, "now https://d.org")], {}, asked)).toEqual([msg(4, "now https://d.org")]);
  });

  it("an edit drops the old preview", () => {
    let state = reduceText(initialTextState, { type: "history", channel: 1, messages: [msg(5, "https://a.org")], more: false });
    state = reduceText(state, { type: "preview", message: 5, preview: { url: "https://a.org", title: "A" } });
    state = reduceText(state, { type: "message-edited", message: msg(5, "https://b.org") });
    expect(5 in state.previews).toBe(false);
  });

  it("keeps the preview when the message comes again with the same text (fresh file links)", () => {
    let state = reduceText(initialTextState, { type: "history", channel: 1, messages: [msg(5, "https://a.org")], more: false });
    state = reduceText(state, { type: "preview", message: 5, preview: { url: "https://a.org", title: "A" } });
    state = reduceText(state, { type: "message-edited", message: msg(5, "https://a.org") });
    expect(state.previews[5]?.title).toBe("A");
  });

  it("keeps the server's answers", () => {
    const ev = parseServerEvent(
      JSON.stringify({ type: "preview", channel: 1, message: 5, preview: { url: "https://a.org", title: "A", image: "/api/link-image/k" } }),
    );
    expect(ev).toEqual({ type: "preview", message: 5, preview: { url: "https://a.org", title: "A", image: "/api/link-image/k" } });
    const sized = { url: "https://a.org", title: "A", image: "/api/link-image/k", imageWidth: 120, imageHeight: 60 };
    expect(parseServerEvent(JSON.stringify({ type: "preview", message: 7, preview: sized }))).toEqual({ type: "preview", message: 7, preview: sized });
    expect(parseServerEvent(JSON.stringify({ type: "preview", message: 7, preview: { ...sized, imageWidth: -1 } }))).toBeNull();
    expect(parseServerEvent('{"type":"preview","message":6,"preview":null}')).toEqual({ type: "preview", message: 6, preview: null });
    expect(parseServerEvent('{"type":"preview","message":6,"preview":{"url":"javascript:x","title":"A"}}')).toBeNull();
    const state = reduceText(initialTextState, ev!);
    expect(state.previews[5]?.title).toBe("A");
  });
});
