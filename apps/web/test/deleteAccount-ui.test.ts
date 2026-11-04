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

const open = (deleteAccounts: boolean, accountUsers: number[] | null = [5]) => {
  const onUnregister = vi.fn(async () => {});
  const noop = vi.fn(async () => {});
  mounted.push(mount(ModerationDialog, { target: document.body, props: {
    bans: null, registered: [{ userId: 5, name: "alice" }, { userId: 6, name: "bob" }], accountUsers,
    rights: { bans: false, editBans: false, users: true, deleteAccounts, emoji: false },
    onLoad: vi.fn(), onSetBans: noop, onRename: noop, onUnregister, emoji: [], onAddEmoji: noop, onDeleteEmoji: noop,
    stickers: [], onAddSticker: noop, onEditSticker: noop, onDeleteSticker: noop, onClose: vi.fn(),
  } }));
  flushSync();
  return onUnregister;
};

const row = (name: string) => [...document.querySelectorAll("li")].find((li) => li.querySelector("strong")?.textContent === name)!;
const buttons = (li: Element) => [...li.querySelectorAll("button")].map((b) => b.textContent?.trim());

describe("deleting accounts in the users list", () => {
  it("offers admins to delete an account, and unregisters Mumble users", () => {
    const onUnregister = open(true);
    expect(buttons(row("alice"))).toEqual(["Delete account…"]);
    expect(buttons(row("bob"))).toEqual(["Rename", "Unregister…"]);
    row("alice").querySelector("button")!.click();
    flushSync();
    expect(row("alice").textContent).toContain("messages stay");
    row("alice").querySelector<HTMLButtonElement>("button.danger")!.click();
    expect(onUnregister).toHaveBeenCalledWith(5);
  });

  it("leaves accounts alone for moderators", () => {
    open(false);
    expect(buttons(row("alice"))).toEqual([]);
    expect(row("alice").textContent).toContain("web account");
  });

  it("offers nothing until it knows the accounts", () => {
    open(true, null);
    expect(buttons(row("alice"))).toEqual([]);
    expect(buttons(row("bob"))).toEqual([]);
  });
});
