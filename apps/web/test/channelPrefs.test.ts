import { describe, expect, it } from "vitest";
import { forgetPref, muteUntil, mutedIds, nothingIds, notifies, parseChannelPrefs, prefOf, pushLists, setChannelPref, setThreadPref, withoutMuted, withParents } from "../src/lib/channelPrefs";

const A = "https://a";

describe("channel settings", () => {
  it("reads stored data, also the old list of muted channels", () => {
    expect(parseChannelPrefs(null)).toEqual({});
    expect(parseChannelPrefs("nope")).toEqual({});
    expect(parseChannelPrefs(JSON.stringify({ [A]: [1, "x", 2], b: "no" }))).toEqual({ [A]: { 1: { mutedUntil: 0 }, 2: { mutedUntil: 0 } } });
    expect(parseChannelPrefs(JSON.stringify({ [A]: { 3: { level: "all", mutedUntil: 99 }, 4: { level: "loud" }, x: {} } }))).toEqual({
      [A]: { 3: { level: "all", mutedUntil: 99 } },
    });
  });

  it("sets per server and channel, and forgets defaults", () => {
    let prefs = setChannelPref({}, A, 3, { mutedUntil: 0 });
    prefs = setChannelPref(prefs, "https://b", 3, { level: "nothing" });
    expect(prefOf(prefs, A, 3)).toEqual({ mutedUntil: 0 });
    prefs = setChannelPref(prefs, A, 3, { mutedUntil: undefined });
    expect(prefs[A]).toEqual({});
    expect(prefOf(prefs, "https://b", 3)).toEqual({ level: "nothing" });
    expect(setChannelPref(prefs, "https://b", 3, { level: "mentions" })["https://b"]).toEqual({});
  });

  it("mutes until a time, or for ever (0)", () => {
    const prefs = { [A]: { 1: { mutedUntil: 0 }, 2: { mutedUntil: 1000 }, 3: { mutedUntil: 500 }, 4: { level: "all" as const } } };
    expect(mutedIds(prefs, A, 800)).toEqual(new Set([1, 2]));
    expect(withoutMuted(new Set([1, 2, 3, 4]), new Set([1, 2]))).toEqual(new Set([3, 4]));
  });

  it("lists the channels set to nothing (no push notifications)", () => {
    const prefs = { [A]: { 1: { level: "nothing" as const }, 2: { mutedUntil: 0 }, 5: { level: "nothing" as const, mutedUntil: 0 } }, b: { 3: { level: "nothing" as const } } };
    expect(nothingIds(prefs, A)).toEqual([1, 5]);
    expect(nothingIds(prefs, "c")).toEqual([]);
  });

  it("notifies by the level; muted: only mentions; nothing: never", () => {
    expect(notifies({}, false, 0)).toBe(false); // only @mentions by default
    expect(notifies({}, true, 0)).toBe(true);
    expect(notifies({ level: "all" }, false, 0)).toBe(true);
    expect(notifies({ level: "all", mutedUntil: 0 }, false, 0)).toBe(false);
    expect(notifies({ level: "all", mutedUntil: 0 }, true, 0)).toBe(true);
    expect(notifies({ level: "all", mutedUntil: 10 }, false, 20)).toBe(true); // mute is over
    expect(notifies({ level: "nothing" }, true, 0)).toBe(false);
  });

  it("gives threads their channel's settings, unless they have their own", () => {
    const prefs = { [A]: { 1: { mutedUntil: 0 }, 2: { level: "all" as const }, 11: { level: "mentions" as const } }, b: { 3: { level: "nothing" as const } } };
    const parents = { 10: { channel: 1 }, 11: { channel: 1 }, 12: { channel: 2 }, 13: { channel: 4 } };
    const all = withParents(prefs, A, parents);
    expect(prefOf(all, A, 10)).toEqual({ mutedUntil: 0 });
    expect(prefOf(all, A, 11)).toEqual({ level: "mentions" }); // its own: not muted
    expect(prefOf(all, A, 12)).toEqual({ level: "all" });
    expect(prefOf(all, A, 13)).toEqual({});
    expect(mutedIds(all, A, 0)).toEqual(new Set([1, 10]));
    expect(all.b).toBe(prefs.b);
    expect(withParents(prefs, A, { 11: { channel: 1 } })).toBe(prefs);
  });

  it("keeps a thread's own setting whole (the default level too), and forgets it", () => {
    let prefs = setThreadPref({}, A, 10, { mutedUntil: 0 }, { mutedUntil: undefined });
    expect(prefOf(prefs, A, 10)).toEqual({ level: "mentions" });
    expect(parseChannelPrefs(JSON.stringify(prefs))).toEqual(prefs);
    prefs = setThreadPref(prefs, A, 10, { level: "mentions" }, { level: "all" });
    expect(prefOf(prefs, A, 10)).toEqual({ level: "all" });
    prefs = setThreadPref(prefs, A, 10, { level: "all" }, { mutedUntil: 5 });
    expect(prefOf(prefs, A, 10)).toEqual({ level: "all", mutedUntil: 5 });
    expect(forgetPref(prefs, A, 10)[A]).toEqual({});
  });

  it("lists for push: quiet chats, and those with a setting of their own (threads of quiet channels)", () => {
    const prefs = { [A]: { 1: { level: "nothing" as const }, 7: { mutedUntil: 0 }, 8: { mutedUntil: 10 }, 11: { level: "mentions" as const } } };
    expect(pushLists(prefs, A, [7, 8], 5)).toEqual({ quiet: [1, 7], loud: [11], all: [] }); // thread settings have a level
    expect(pushLists({}, A, [7], 5)).toEqual({ quiet: [], loud: [], all: [] });
  });

  it("lists for push the chats set to all messages, but not the muted ones", () => {
    const all = "all" as const;
    const prefs = { [A]: { 2: { level: all }, 3: { level: all, mutedUntil: 0 }, 4: { level: all, mutedUntil: 99 }, 5: { level: all, mutedUntil: 3 } } };
    expect(pushLists(prefs, A, [], 5)).toEqual({ quiet: [], loud: [2, 3, 4, 5], all: [2, 5] }); // 5: the mute is over
  });
});

describe("mute choices", () => {
  it("mute for some minutes, or until turned off (0)", () => {
    expect(muteUntil(15, 1_000)).toBe(1_000 + 15 * 60_000);
    expect(muteUntil(0, 1_000)).toBe(0);
  });
});
