import { describe, expect, it } from "vitest";
import { fileLinksRequest, fileLinksToAsk, linkKey } from "../src/lib/fileLinks";
import type { TextMessage } from "../src/lib/textChannels";

const hour = 3_600_000;
const now = 1_800_000_000_000;
const signed = (ms: number, preview = false) => `/files/k/a.png?${preview ? "preview&" : ""}ex=${Math.floor(ms / 1000)}&sig=s`;
const msg = (id: number, url: string, preview?: string): TextMessage => ({
  id,
  channel: 1,
  author: "a",
  text: "",
  at: 1,
  uploads: [{ url, name: "a.png", type: "image/png", size: 1, ...(preview && { preview }) }],
});

describe("signed file links", () => {
  it("asks again for messages whose links expire within the hour, once per link", () => {
    const fresh = msg(1, signed(now + 20 * hour));
    const soon = msg(2, signed(now + 30 * 60_000));
    const gone = msg(3, signed(now - hour));
    const previewSoon = msg(4, signed(now + 20 * hour), signed(now + 60_000, true));
    const plain = msg(5, "/files/k/a.png");
    const text: TextMessage = { id: 6, channel: 1, author: "a", text: "hi", at: 1 };
    const asked = new Set([linkKey(gone)]);
    expect(fileLinksToAsk([fresh, soon, gone, previewSoon, plain, text], asked, now).map((m) => m.id)).toEqual([2, 4]);
    // Its new links are another key.
    expect(linkKey(msg(3, signed(now + 24 * hour)))).not.toBe(linkKey(gone));
  });

  it("is a request with the message ids", () => {
    expect(JSON.parse(fileLinksRequest([2, 4]))).toEqual({ type: "file-links", messages: [2, 4] });
  });
});
