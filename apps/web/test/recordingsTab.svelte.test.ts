// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, expect, it, vi } from "vitest";
import Harness from "./helpers/RecordingsHarness.svelte";
import { t } from "../src/lib/i18n.svelte";

HTMLDialogElement.prototype.showModal = function () {
  this.open = true;
};
HTMLDialogElement.prototype.close = function () {
  this.open = false;
};

const mounted: ReturnType<typeof mount>[] = [];
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
});
const settle = async () => {
  for (let i = 0; i < 20; i++) (flushSync(), await Promise.resolve());
};

// GitHub #19: the tab asked again with every answer, so the rate limit
// then refused the download token.
it("asks for the recordings once when the tab opens, not again with each answer", async () => {
  const onAsk = vi.fn();
  const props = $state({ onAsk, recorded: [] as number[] });
  mounted.push(mount(Harness, { target: document.body, props }));
  flushSync();
  [...document.querySelectorAll("button")].find((b) => b.textContent?.trim() === t("admin.sectionRecordings"))!.click();
  await settle();
  expect(onAsk).toHaveBeenCalledOnce();
  // A recording starts: the list changes, asked again once.
  props.recorded = [5];
  await settle();
  expect(onAsk).toHaveBeenCalledTimes(2);
});
