// @vitest-environment jsdom
import { flushSync } from "svelte";
import { expect, it, vi } from "vitest";
import { show } from "./helpers/composer.svelte";

it("a link card whose image is gone drops it and asks for the card again", () => {
  const onPreviewGone = vi.fn();
  show({
    previewOf: (id) =>
      id === 2
        ? {
            url: "https://example.com/",
            title: "Example",
            image: "/api/link-image/old",
          }
        : null,
    httpBase: "",
    onPreviewGone,
  });
  const img = document.querySelector<HTMLImageElement>(
    '[data-message="2"] a.preview img',
  )!;
  img.dispatchEvent(new Event("error"));
  flushSync();
  expect(document.querySelector('[data-message="2"] a.preview img')).toBeNull();
  expect(onPreviewGone).toHaveBeenCalledWith(2);
});
