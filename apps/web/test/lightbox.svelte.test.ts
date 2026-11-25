// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import Lightbox from "../src/components/Lightbox.svelte";

// jsdom has no modal dialogs.
HTMLDialogElement.prototype.showModal = function () { this.open = true; };
HTMLDialogElement.prototype.close = function () { this.open = false; };

type Item = { url: string; name: string; video?: boolean };
const item = (url: string, video?: boolean): Item => ({ url, name: url, ...(video && { video }) });
let shown: ReturnType<typeof mount> | undefined;
afterEach(async () => {
  if (shown) await unmount(shown);
  document.body.replaceChildren();
});

const open = (items: Item[], start = 0) => {
  const props = $state({ items, start, more: true, loading: false, onOlder: vi.fn(), onClose: () => {} });
  shown = mount(Lightbox, { target: document.body, props });
  flushSync();
  return props;
};
const media = () => document.querySelector<HTMLImageElement | HTMLVideoElement>("dialog img, dialog video")!;
const count = () => document.querySelector(".count")?.textContent;
const key = (k: string) => {
  window.dispatchEvent(new KeyboardEvent("keydown", { key: k }));
  flushSync();
};

describe("Lightbox", () => {
  it("plays a video", () => {
    open([item("p.png"), item("v.mp4", true)], 1);
    expect(media()).toBeInstanceOf(HTMLVideoElement);
    expect((media() as HTMLVideoElement).controls).toBe(true);
  });
});
