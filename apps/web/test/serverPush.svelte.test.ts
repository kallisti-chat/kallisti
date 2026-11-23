// @vitest-environment jsdom
import { flushSync } from "svelte";
import { describe, expect, it } from "vitest";
import { createServerPush } from "../src/lib/serverPush.svelte";

const tick = () => new Promise((r) => setTimeout(r, 0));

const setup = () => {
  let on = true;
  let loaded = $state(false);
  let servers = $state.raw([{ origin: "https://b.example", name: "al", token: "tb" as string | null }]);
  const synced: [string, boolean][] = [];
  const kept: string[][] = [];
  const push = createServerPush({
    name: () => "alice",
    loaded: () => loaded,
    servers: () => servers,
    isOn: async () => on,
    sync: async (s, homeOn) => void synced.push([s.origin, homeOn]),
    drop: async (keep) => void kept.push([...keep]),
  });
  flushSync();
  return { push, synced, kept, set: (v: boolean) => (on = v), load: () => (loaded = true), remove: () => (servers = []) };
};

describe("push from added servers", () => {
  it("follows the home server's push, here and from another tab", async () => {
    const { push, synced, set } = setup();
    await tick();
    push.syncFor({ origin: "https://b.example", name: "al", token: "tb" })({ quiet: [], loud: [], all: [], blocked: [] });
    set(false);
    dispatchEvent(new StorageEvent("storage", { key: "pushOwner" })); // another tab turned it off
    await tick();
    push.syncFor({ origin: "https://b.example", name: "al", token: "tb" })({ quiet: [], loud: [], all: [], blocked: [] });
    expect(synced).toEqual([["https://b.example", true], ["https://b.example", false]]);
    push.dispose();
  });

  it("drops the push of servers that went, only once the list is known", () => {
    const { push, kept, load, remove } = setup();
    expect(kept).toEqual([]); // not before the list: all would look gone
    load();
    flushSync();
    remove();
    flushSync();
    expect(kept).toEqual([["https://b.example"], []]);
    push.dispose();
  });
});
