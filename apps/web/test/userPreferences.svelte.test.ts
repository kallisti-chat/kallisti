// @vitest-environment jsdom
import { flushSync } from "svelte";
import { bridgeUrl, kallistiSocket } from "../src/lib/desktop";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { chatItems } from "../src/lib/chat";
import { mentionCounts } from "../src/lib/mentions";
import { initialTextState } from "../src/lib/textChannels";
import { gainOf } from "../src/lib/userAudio";
import { bindUserPreferences, parseUserPreferences, preferenceScope, type UserPreferences } from "../src/lib/userPreferences";

beforeEach(() => { localStorage.clear(); vi.resetModules(); });

const policies = async () => {
  const { userPreferences } = await import("../src/lib/settings.svelte");
  return [userPreferences.forServer("https://a.example"), userPreferences.forServer("https://b.example")] as const;
};

describe("server-bound user preferences", () => {
  it("canonicalizes Kallisti origins, direct or bridged", () => {
    expect(preferenceScope("wss://EXAMPLE.com:443/ws")).toBe("https://example.com");
    expect(preferenceScope("ws://127.0.0.1:1234/?kallisti=wss://example.com/ws&secret=x", "https://EXAMPLE.com:443/path")).toBe("https://example.com");
    expect(preferenceScope("wss://example.com/ws", "https://EXAMPLE.com/path")).toBe("https://example.com");
  });

  it("uses the real plain-Mumble endpoint across bridge restarts", () => {
    const bridge = (port: number, host: string, targetPort = 64738) =>
      bridgeUrl({ port, secret: "temporary" }, { host, port: targetPort });
    expect(preferenceScope(bridge(1234, "Voice.EXAMPLE"))).toBe("mumble://voice.example:64738");
    expect(preferenceScope(bridge(9999, "voice.example"))).toBe("mumble://voice.example:64738");
    expect(preferenceScope(bridge(1234, "voice.example", 64739))).not.toBe(preferenceScope(bridge(1234, "voice.example")));
    expect(preferenceScope(bridge(1234, "2001:DB8::1"))).toBe("mumble://[2001:db8::1]:64738");
    expect(preferenceScope(bridge(1234, "other.example"))).not.toBe(preferenceScope(bridge(1234, "voice.example")));
  });

  it("shares Kallisti preferences across transports, but not with plain Mumble on that host", async () => {
    const { userPreferences } = await import("../src/lib/settings.svelte");
    const direct = kallistiSocket("https://voice.example", null);
    const bridged = kallistiSocket("https://voice.example", null, { bridge: { port: 1234, secret: "x" }, udpPort: 64738 });
    const a = userPreferences.forServer(preferenceScope(direct.url));
    const b = userPreferences.forServer(preferenceScope(bridged.url, bridged.site));
    const plain = userPreferences.forServer(preferenceScope(bridgeUrl({ port: 1234, secret: "x" }, { host: "voice.example", port: 64738 })));
    a.toggle("Bob");
    a.updateAudio("Bob", { muted: true, volume: 0.3 });
    expect(b.has("Bob")).toBe(true);
    expect(b.audioOf("Bob")).toEqual({ muted: true, volume: 0.3 });
    expect(plain.has("Bob")).toBe(false);
    expect(plain.audioOf("Bob")).toEqual({ muted: false, volume: 1 });
  });

  it("updates headless consumers after block and audio changes", () => {
    let value = $state.raw<UserPreferences>({});
    const store = { get value() { return value; }, set: (next: UserPreferences) => { value = next; } };
    const a = bindUserPreferences(store, "https://a.example");
    const b = bindUserPreferences(store, "https://b.example");
    const observed: { a: string[]; b: string[]; gain: number }[] = [];
    const stop = $effect.root(() => {
      $effect(() => { observed.push({ a: [...a.list], b: [...b.list], gain: gainOf(a.audioOf("Bob")) }); });
    });
    try {
      flushSync();
      a.toggle("Bob");
      a.updateAudio("Bob", { volume: 0.6 });
      flushSync();
      expect(observed.at(-1)).toEqual({ a: ["bob"], b: [], gain: 0.6 });
      a.updateAudio("Bob", { muted: true });
      b.toggle("Bob");
      flushSync();
      expect(observed.at(-1)).toEqual({ a: ["bob"], b: ["bob"], gain: 0 });
      a.toggle("BOB");
      a.updateAudio("Bob", { muted: false, volume: 1 });
      flushSync();
      expect(observed.at(-1)).toEqual({ a: [], b: ["bob"], gain: 1 });
    } finally { stop(); }
  });

  it("keeps same-name blocks, mute and volume independent and persists them", async () => {
    const [a, b] = await policies();
    a.toggle("Bob");
    a.updateAudio("Bob", { muted: true, volume: 0.4 });
    b.updateAudio("Bob", { volume: 1.7 });
    expect(a.has("BOB")).toBe(true);
    expect(b.has("Bob")).toBe(false);
    expect(a.has(null)).toBe(false);
    expect(gainOf(a.audioOf("bob"))).toBe(0);
    expect(b.audioOf("BOB")).toEqual({ muted: false, volume: 1.7 });
    vi.resetModules();
    const [restoredA, restoredB] = await policies();
    expect(restoredA.list).toEqual(["bob"]);
    expect(restoredA.audioOf("Bob")).toEqual({ muted: true, volume: 0.4 });
    expect(restoredB.list).toEqual([]);
    expect(restoredB.audioOf("Bob").volume).toBe(1.7);
    restoredA.toggle("BOB");
    expect(restoredA.has("bob")).toBe(false);
  });

  it("does not assign ambiguous legacy data to any server or erase it", async () => {
    localStorage.setItem("blockedUsers", '["bob"]');
    localStorage.setItem("userAudio", '{"Bob":{"muted":true,"volume":0.2}}');
    const [a, b] = await policies();
    for (const p of [a, b]) {
      expect(p.has("Bob")).toBe(false);
      expect(p.audioOf("Bob")).toEqual({ muted: false, volume: 1 });
    }
    a.toggle("Alice");
    vi.resetModules();
    const [again, other] = await policies();
    expect(again.has("Alice")).toBe(true);
    expect(other.has("Alice")).toBe(false);
    expect(localStorage.getItem("blockedUsers")).toBe('["bob"]');
    expect(JSON.parse(localStorage.getItem("userAudio")!).Bob.muted).toBe(true);
  });

  it("uses the bound policy for quotes, groups, mentions, search, pins and push lists", async () => {
    const [a, b] = await policies();
    a.toggle("Bob");
    const messages = [{ id: 1, channel: 7, author: "Bob", text: "@me hello", at: 1 }];
    const state = { ...initialTextState, channels: [{ id: 7, name: "chat" }], messages: { 7: messages } };
    const groups = messages.map((m) => ({ ...m, actor: 2, actorName: m.author, channelIds: [7], treeIds: [], sessions: [], peer: null }));
    for (const [p, hidden] of [[a, true], [b, false]] as const) {
      expect(p.has(messages[0]!.author)).toBe(hidden); // Quote author.
      expect(chatItems(groups, p.has).some((item) => item.kind === "blocked")).toBe(hidden);
      expect(mentionCounts(state, {}, "me", p.has)).toEqual(hidden ? {} : { 7: 1 });
      expect(p.visible(messages)).toEqual(hidden ? [] : messages); // Search and pins.
      expect(JSON.parse(JSON.stringify({ blocked: [...p.list].sort() }))).toEqual({ blocked: hidden ? ["bob"] : [] });
    }
  });

  it("rejects unversioned or malformed data and validates scoped entries", () => {
    for (const raw of [null, "broken", "[]", "null", '{"bob":{"muted":true}}', '{"version":2,"servers":{}}', '{"version":1,"servers":[]}']) {
      expect(parseUserPreferences(raw)).toEqual({});
    }
    expect(parseUserPreferences(JSON.stringify({ version: 1, servers: {
      "https://a.example": { blocked: ["Bob", 5], audio: { bob: { muted: true, volume: 9 } } },
      "https://b.example": null,
    } }))).toEqual({ "https://a.example": { blocked: ["bob"], audio: { bob: { muted: true, volume: 2 } } } });
  });

  it("handles broken scoped storage and storage events without importing legacy values", async () => {
    localStorage.setItem("serverUserPreferences", "broken");
    const [a, b] = await policies();
    expect(a.list).toEqual([]);
    a.toggle("Bob");
    const raw = localStorage.getItem("serverUserPreferences")!;
    window.dispatchEvent(new StorageEvent("storage", { key: "serverUserPreferences", newValue: null }));
    expect(a.list).toEqual([]);
    window.dispatchEvent(new StorageEvent("storage", { key: "serverUserPreferences", newValue: raw }));
    expect(a.has("Bob")).toBe(true);
    expect(b.has("Bob")).toBe(false);
  });
});
