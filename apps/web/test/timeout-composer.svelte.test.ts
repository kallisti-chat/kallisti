// @vitest-environment jsdom
import { flushSync, type ComponentProps } from "svelte";
import type ChatPane from "../src/components/ChatPane.svelte";
import { expect, it, vi } from "vitest";
import { flush, input, show, submit } from "./helpers/composer.svelte";

it("blocks button/submit/keyboard, keeps draft and reply, then permits writes after expiry", () => {
  const s = show();
  document.querySelector<HTMLButtonElement>('[data-message="1"] button[aria-label="Reply"]')!.click();
  const box = document.querySelector<HTMLTextAreaElement>(".composer textarea")!;
  input(box, "unsent"); s.block("Timed out");
  const button = document.querySelector<HTMLButtonElement>(".send-button")!;
  expect(button.disabled).toBe(true); button.click();
  submit(); box.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  expect(s.onSend).not.toHaveBeenCalled(); expect(box.value).toBe("unsent");
  s.block(null); button.click(); expect(s.onSend).toHaveBeenCalledWith("unsent", 1, false);
});

it("blocks queued uploads and file picker/paste/drop paths, retaining files and caption", async () => {
  const onUpload = vi.fn<NonNullable<ComponentProps<typeof ChatPane>["onUpload"]>>(async () => {});
  const s = show({ onUpload });
  const box = document.querySelector<HTMLTextAreaElement>(".composer textarea")!;
  input(box, "caption");
  const file = new File(["fake"], "draft.txt", { type: "text/plain" });
  const picker = document.querySelector<HTMLInputElement>('input[type="file"]')!;
  Object.defineProperty(picker, "files", { configurable: true, value: [file] });
  picker.dispatchEvent(new Event("change", { bubbles: true })); flushSync();
  s.block("Timed out"); submit();
  expect(onUpload).not.toHaveBeenCalled(); expect(box.value).toBe("caption");
  expect(document.querySelector<HTMLButtonElement>("button.attach")!.disabled).toBe(true);
  const blocked = new File(["second"], "blocked.txt");
  Object.defineProperty(picker, "files", { value: [blocked] });
  picker.dispatchEvent(new Event("change", { bubbles: true }));
  const paste = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(paste, "clipboardData", { value: { files: [blocked], items: [], types: ["Files"] } }); box.dispatchEvent(paste);
  const drop = new Event("drop", { bubbles: true, cancelable: true });
  Object.defineProperty(drop, "dataTransfer", { value: { files: [blocked] } }); box.dispatchEvent(drop); flushSync();
  expect(document.body.textContent).not.toContain("blocked.txt");
  s.block(null); submit(); await flush();
  expect(onUpload).toHaveBeenCalledOnce();
  expect(onUpload.mock.calls[0]![0]).toEqual([{ file, description: undefined }]);
  expect(onUpload.mock.calls[0]![1]).toBe("caption");
});

it("guards an already-open sticker picker, retaining draft and reply", async () => {
  const onSticker = vi.fn(async () => {});
  const s = show({ onSticker, stickers: [{ id: 3, name: "cat", url: "/fake.png" }] });
  document.querySelector<HTMLButtonElement>('[data-message="1"] button[aria-label="Reply"]')!.click();
  const box = document.querySelector<HTMLTextAreaElement>(".composer textarea")!; input(box, "unsent");
  document.querySelector<HTMLButtonElement>(".sticker-button")!.click(); flushSync();
  s.block("Timed out");
  expect(document.querySelector<HTMLButtonElement>(".sticker-button")!.disabled).toBe(true);
  document.querySelector<HTMLButtonElement>("button.cell")!.click(); await flush();
  expect(onSticker).not.toHaveBeenCalled(); expect(box.value).toBe("unsent");
  s.block(null); document.querySelector<HTMLButtonElement>(".sticker-button")!.click(); flushSync();
  document.querySelector<HTMLButtonElement>("button.cell")!.click(); await flush();
  expect(onSticker).toHaveBeenCalledWith(3, 1, false); expect(box.value).toBe("unsent");
});

it("guards an open poll form and allows its unchanged draft after expiry", async () => {
  const onPoll = vi.fn(async () => {}), s = show({ onPoll });
  document.querySelector<HTMLButtonElement>("button.attach")!.click(); flushSync();
  [...document.querySelectorAll<HTMLButtonElement>(".plus-menu button")].find((b) => b.textContent?.includes("poll"))!.click(); flushSync();
  const form = document.querySelector<HTMLFormElement>("dialog form")!;
  const fields = [...form.querySelectorAll<HTMLInputElement>('input:not([type="checkbox"])')];
  for (const [i, field] of fields.entries()) {
    field.value = ["Question", "Yes", "No"][i]!; field.dispatchEvent(new Event("input", { bubbles: true }));
  }
  flushSync(); s.block("Timed out");
  expect(form.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(true);
  form.dispatchEvent(new Event("submit", { cancelable: true })); await flush(); expect(onPoll).not.toHaveBeenCalled();
  expect(fields[0]!.value).toBe("Question");
  s.block(null); form.dispatchEvent(new Event("submit", { cancelable: true })); await flush(); expect(onPoll).toHaveBeenCalledOnce();
});
