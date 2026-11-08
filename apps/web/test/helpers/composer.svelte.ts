// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, vi } from "vitest";
import ChatPane from "../../src/components/ChatPane.svelte";
import type { ComponentProps } from "svelte";
import type { DisplayMessage } from "../../src/lib/textChannels";
const mounted: ReturnType<typeof mount>[] = [];
const message = (id: number): DisplayMessage => ({ actor: 1, actorName: "me", channelIds: [], treeIds: [], sessions: [], text: "original", at: id, peer: null, source: { id, text: "original" }, uploads: [{ url: "/files/key/a.txt", name: "a.txt", size: 1, type: "text/plain" }] });
export const flush = async () => { flushSync(); await Promise.resolve(); flushSync(); };
export const input = (el: HTMLTextAreaElement, text: string) => { el.value = text; el.dispatchEvent(new Event("input", { bubbles: true })); flushSync(); };
export const submit = (selector = "form.send") => { document.querySelector(selector)!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); flushSync(); };
export const show = (extra: Partial<ComponentProps<typeof ChatPane>> = {}) => {
  let blocked = $state<string | null>(null);
  const onSend = vi.fn();
  const props = { title: "general", kind: "text" as const, messages: [message(1), message(2)], welcomeText: "", membersOpen: false, onToggleMembers() {}, blocked: (_name: string | null) => false, me: "me", myId: 1, onSend, mayEdit: () => true, maxUploads: 2, ...extra, get cannotWrite() { return blocked; } };
  const target = document.createElement("div"); document.body.append(target);
  mounted.push(mount(ChatPane, { target, props })); flushSync();
  return { onSend, block: (reason: string | null) => { blocked = reason; flushSync(); } };
};
export const edit = (id: number) => { document.querySelector<HTMLButtonElement>(`[data-message="${id}"] .actions button[aria-label="Edit message"]`)!.click(); flushSync(); };
beforeEach(() => {
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
  vi.stubGlobal("IntersectionObserver", class { observe() {} disconnect() {} unobserve() {} });
  HTMLElement.prototype.scrollIntoView = vi.fn();
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new Event("close")); };
});
afterEach(async () => { for (const c of mounted.splice(0)) await unmount(c); document.body.replaceChildren(); vi.unstubAllGlobals(); });
