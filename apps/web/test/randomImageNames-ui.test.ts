// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import ModerationDialog from "../src/components/ModerationDialog.svelte";

HTMLDialogElement.prototype.showModal = function () { this.open = true; };
HTMLDialogElement.prototype.close = function () { this.open = false; };
const mounted: ReturnType<typeof mount>[] = [];
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
});

const render = (value: boolean | null, save?: (on: boolean) => Promise<void>) => {
  const noop = async () => {};
  mounted.push(mount(ModerationDialog, { target: document.body, props: {
    bans: [], registered: [], rights: { bans: false, editBans: false, users: false, emoji: false },
    onLoad: vi.fn(), onSetBans: noop, onUnregister: noop, emoji: [], onAddEmoji: noop, onDeleteEmoji: noop,
    stickers: [], onAddSticker: noop, onEditSticker: noop, onDeleteSticker: noop, onClose: vi.fn(),
    serverName: "test", onServerName: noop, randomImageNames: value, onRandomImageNames: save,
  } }));
  flushSync();
};
const checkbox = () => document.querySelector<HTMLInputElement>('input[type="checkbox"]');

describe("random image names control", () => {
  it("hides the control when the setting is unavailable or cannot be changed", () => {
    render(null, async () => {});
    expect(checkbox()).toBeNull();
    render(false);
    expect(checkbox()).toBeNull();
  });

  it("waits for acknowledgement while saving and keeps the new choice", async () => {
    let resolve!: () => void;
    const save = vi.fn(() => new Promise<void>((ok) => { resolve = ok; }));
    render(false, save);
    const input = checkbox()!;
    expect(input.checked).toBe(false);
    input.click();
    flushSync();
    expect(save).toHaveBeenCalledWith(true);
    expect(input.disabled).toBe(true);
    resolve();
    await Promise.resolve();
    flushSync();
    expect(input.disabled).toBe(false);
    expect(input.checked).toBe(true);
    expect(document.body.textContent).toContain("Saved");
  });

  it("restores the server value when saving fails", async () => {
    const save = vi.fn(async () => { throw new Error("not allowed"); });
    render(true, save);
    const input = checkbox()!;
    input.click();
    expect(save).toHaveBeenCalledWith(false);
    await Promise.resolve();
    flushSync();
    expect(input.checked).toBe(true);
    expect(input.disabled).toBe(false);
    expect(document.body.textContent).toContain("not allowed");
  });
});
