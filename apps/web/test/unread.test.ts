import { describe, expect, it } from "vitest";
import { firstUnread, nextLines, unreadBar } from "../src/lib/unread";

const msg = (id: number, by = "bob", at = id * 60_000) => ({ source: { id }, actorName: by, at });
const mine = (m: { actorName: string | null }) => m.actorName === "me";

describe("firstUnread", () => {
  it("is the first message after the mark that someone else wrote", () => {
    const ms = [msg(1), msg(2), msg(3, "me"), msg(4)];
    expect(firstUnread(ms, 2, mine)).toBe(4);
    expect(firstUnread(ms, 1, mine)).toBe(2);
    expect(firstUnread(ms, 4, mine)).toBeNull();
    expect(firstUnread(ms, null, mine)).toBeNull();
  });
  it("skips lines without an id (Mumble chat)", () => {
    expect(firstUnread([{ actorName: "x", at: 0 }, msg(5)], 1, mine)).toBe(5);
  });
});

describe("unreadBar", () => {
  it("counts the unread messages of others since the first one", () => {
    const bar = unreadBar([msg(1), msg(2), msg(3, "me"), msg(4)], 1, mine, (at) => `t${at}`);
    expect(bar).toEqual({ first: 2, text: "2 new messages since t120000" });
    expect(unreadBar([msg(1), msg(2)], 1, mine, String)?.text).toBe("1 new message since 120000");
  });
  it("says at least the loaded count when the first unread one is not loaded", () => {
    expect(unreadBar([msg(10), msg(11)], 3, mine, String)).toEqual({ first: null, text: "2+ new messages" });
  });
  it("is none without unread messages", () => {
    expect(unreadBar([msg(1, "me")], 0, mine, String)).toBeNull();
    expect(unreadBar([msg(1)], null, mine, String)).toBeNull();
  });
});

describe("nextLines", () => {
  const base = { open: [1, 2], watched: new Set<number>(), seen: [] as [number, number][], read: { 1: 5, 2: 0 } };
  it("puts a line where the chat was read when it comes into view with unread messages", () => {
    expect(nextLines({}, { ...base, seen: [[1, 9]] })).toEqual({ 1: 5 });
  });
  it("keeps it while the chat is open, and sets none while it is watched", () => {
    expect(nextLines({ 1: 5 }, { ...base, read: { 1: 9 }, seen: [[1, 12]], watched: new Set([1]) })).toEqual({ 1: 5 });
    expect(nextLines({}, { ...base, seen: [[1, 9]], watched: new Set([1]) })).toEqual({});
  });
  it("sets none without an earlier read mark, and drops lines of closed chats", () => {
    expect(nextLines({}, { ...base, seen: [[2, 9]] })).toEqual({});
    expect(nextLines({ 1: 5, 3: 2 }, base)).toEqual({ 1: 5 });
  });
});
