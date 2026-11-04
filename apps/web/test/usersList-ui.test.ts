// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import ModerationDialog from "../src/components/ModerationDialog.svelte";

// jsdom has no modal dialogs.
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

const row = (name: string) => [...document.querySelectorAll("li")].find((li) => li.querySelector("strong")?.textContent === name)!;
const buttons = (li: Element) => [...li.querySelectorAll("button")].map((b) => b.textContent?.trim());

describe("the users list", () => {
  it("deletes a user after a confirmation, and renames nobody", () => {
    const onUnregister = vi.fn(async () => {});
    const noop = vi.fn(async () => {});
    mounted.push(mount(ModerationDialog, { target: document.body, props: {
      bans: null, registered: [{ userId: 0, name: "SuperUser" }, { userId: 5, name: "alice" }],
      rights: { bans: false, editBans: false, users: true, emoji: false },
      onLoad: vi.fn(), onSetBans: noop, onUnregister, emoji: [], onAddEmoji: noop, onDeleteEmoji: noop,
      stickers: [], onAddSticker: noop, onEditSticker: noop, onDeleteSticker: noop, onClose: vi.fn(),
    } }));
    flushSync();
    expect(buttons(row("SuperUser"))).toEqual([]);
    expect(buttons(row("alice"))).toEqual(["Delete user…"]);
    row("alice").querySelector("button")!.click();
    flushSync();
    expect(row("alice").textContent).toContain("account and roles go; messages stay");
    row("alice").querySelector<HTMLButtonElement>("button.danger")!.click();
    expect(onUnregister).toHaveBeenCalledWith(5);
  });
});
