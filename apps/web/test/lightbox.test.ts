import type { ChatMessage } from "@kallisti/protocol";
import { describe, expect, it } from "vitest";
import { chatItems, type ChatItem } from "../src/lib/chat";
import { chatMedia, opensLightbox, viewerStep } from "../src/lib/lightbox";

const click = (o: Partial<MouseEvent> = {}) =>
  ({ button: 0, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, ...o }) as MouseEvent;

describe("opensLightbox", () => {
  it("opens on a plain left click", () => {
    expect(opensLightbox(click())).toBe(true);
  });

  it("leaves new-tab and other clicks to the browser", () => {
    for (const o of [{ button: 1 }, { ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { altKey: true }]) {
      expect(opensLightbox(click(o))).toBe(false);
    }
  });
});

describe("chatMedia", () => {
  type U = { url: string; type: string; spoiler?: true };
  const file = (url: string, type = "image/png", spoiler?: true): U => ({ url, type, ...(spoiler && { spoiler }) });
  const msg = (actorName: string, at: number, uploads: U[]) =>
    ({ actor: null, actorName, channelIds: [], treeIds: [], sessions: [], text: "", at, uploads }) as ChatMessage;
  const view = (u: U) => u.type.split("/")[0]!;
  const urls = (items: ChatItem[], opened = () => false, hidden = (u: U) => !!u.spoiler) =>
    chatMedia<U>(items, opened, view, hidden).map((u) => u.url);

  it("goes through the pictures and videos of all messages as shown", () => {
    const items = chatItems([
      msg("a", 1, [file("v", "video/mp4"), file("p1"), file("p2")]), // a grid: its pictures first
      msg("b", 2, [file("doc", "application/pdf"), file("song", "audio/ogg")]),
      msg("a", 3, [file("p3")]),
    ]);
    expect(urls(items)).toEqual(["p1", "p2", "v", "p3"]);
  });

  it("leaves out hidden spoilers and blocked users' messages not shown", () => {
    const items = chatItems([msg("a", 1, [file("p1"), file("s", "image/png", true)]), msg("bad", 2, [file("p2")])], (n) => n === "bad");
    expect(urls(items)).toEqual(["p1"]);
    expect(urls(items, () => true, () => false)).toEqual(["p1", "s", "p2"]);
  });
});

describe("viewerStep", () => {
  it("asks for older messages before the first item when there are more", () => {
    expect(viewerStep(0, -1, 3, true)).toBe("older");
    expect(viewerStep(0, -1, 3, false)).toBe(0);
    expect(viewerStep(1, -1, 3, true)).toBe(0);
  });
});
