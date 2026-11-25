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

  it("leaves the arrow keys to a video that has the focus", () => {
    open([item("p.png"), item("v.mp4", true)], 1);
    const e = new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true, cancelable: true });
    media().dispatchEvent(e);
    flushSync();
    expect([media().getAttribute("src"), e.defaultPrevented]).toEqual(["v.mp4", false]);
  });

  it("keeps the item shown while older ones come in", () => {
    const props = open([item("b"), item("c")], 1);
    props.items = [item("a"), item("b"), item("c")];
    flushSync();
    expect([media().getAttribute("src"), count()]).toEqual(["c", "3 of 3"]);
  });

  it("loads older messages before the first item, then steps on into them", () => {
    const props = open([item("b"), item("c")]);
    key("ArrowLeft");
    expect(props.onOlder).toHaveBeenCalledOnce();
    props.loading = true;
    flushSync();
    props.items = [item("a"), item("b"), item("c")];
    props.loading = false;
    flushSync();
    expect([media().getAttribute("src"), count()]).toEqual(["a", "1 of 3"]);
  });
});
