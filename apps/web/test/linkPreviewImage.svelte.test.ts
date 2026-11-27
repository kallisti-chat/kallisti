// @vitest-environment jsdom
import { flushSync } from "svelte";
import { expect, it, vi } from "vitest";
import type { LinkPreview } from "../src/lib/linkPreview";
import { show } from "./helpers/composer.svelte";

const card = (image: string): LinkPreview => ({
  url: "https://example.com/",
  title: "Example",
  image,
});
const img = () =>
  document.querySelector<HTMLImageElement>('[data-message="2"] a.preview img');

it("a link card whose image is gone asks again and shows the new image", () => {
  let preview = $state(card("/api/link-image/old"));
  const onPreviewGone = vi.fn();
  show({ previewOf: (id) => (id === 2 ? preview : null), onPreviewGone });

  img()!.dispatchEvent(new Event("error"));
  flushSync();
  expect(img()).toBeNull();
  expect(onPreviewGone).toHaveBeenCalledWith(2);

  preview = card("/api/link-image/new");
  flushSync();
  expect(img()?.getAttribute("src")).toBe("/api/link-image/new");
});
