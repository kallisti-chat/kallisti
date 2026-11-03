import type { ChatMessage } from "@kallisti/protocol";
import { describe, expect, it } from "vitest";
import { chatItems, formatDay, formatTime, formatTimestamp, groupMessages } from "../src/lib/chat";
import type { DisplayMessage } from "../src/lib/textChannels";

const at = (y: number, m: number, d: number, h = 12, min = 0) => new Date(y, m - 1, d, h, min).getTime();
const now = at(2026, 10, 14, 16, 0);
const L = "en-US";

describe("formatTimestamp", () => {
  it("says Today and Yesterday for recent messages", () => {
    expect(formatTimestamp(at(2026, 10, 14, 15, 58), now, L)).toBe("Today at 3:58 PM");
    expect(formatTimestamp(at(2026, 10, 13, 9, 5), now, L)).toBe("Yesterday at 9:05 AM");
  });

  it("uses the date for older messages", () => {
    expect(formatTimestamp(at(2026, 10, 1, 9, 5), now, L)).toBe("10/1/2026 9:05 AM");
  });

  it("writes 24-hour or 12-hour time when set, whatever the language", () => {
    expect(formatTimestamp(at(2026, 10, 14, 15, 58), now, L, "24")).toBe("Today at 15:58");
    expect(formatTimestamp(at(2026, 10, 13, 9, 5), now, L, "24")).toBe("Yesterday at 09:05");
    expect(formatTimestamp(at(2026, 10, 14, 0, 30), now, L, "24")).toBe("Today at 00:30");
    expect(formatTimestamp(at(2026, 10, 14, 15, 58), now, "de-DE", "12")).toBe("Today at 3:58 PM");
    expect(formatTimestamp(at(2026, 10, 14, 15, 58), now, "de-DE", "auto")).toBe("Today at 15:58");
  });
});

describe("formatTime", () => {
  it("follows the time format", () => {
    expect(formatTime(at(2026, 10, 14, 19, 5), L)).toBe("7:05 PM");
    expect(formatTime(at(2026, 10, 14, 19, 5), L, "24")).toBe("19:05");
  });
});

describe("formatDay", () => {
  it("writes the full date", () => {
    expect(formatDay(at(2026, 10, 14), L)).toBe("October 14, 2026");
  });
});

const msg = (actor: number, t: number, text = "x"): ChatMessage => ({
  actor,
  actorName: `u${actor}`,
  channelIds: [0],
  treeIds: [],
  sessions: [],
  text,
  at: t,
    peer: null,
});

describe("groupMessages across midnight", () => {
  it("starts a new group on a new day", () => {
    const groups = groupMessages([msg(1, at(2026, 10, 13, 23, 59)), msg(1, at(2026, 10, 14, 0, 1))]);
    expect(groups).toHaveLength(2);
  });
  it("keeps a webhook's post apart from a user of its name", () => {
    const t = at(2026, 10, 14, 12, 0);
    const hook = (t: number): DisplayMessage => ({ ...msg(1, t), webhook: 3 });
    expect(groupMessages([msg(1, t), hook(t + 1000), hook(t + 2000)])).toHaveLength(2);
  });
});

describe("chatItems", () => {
  it("puts a date divider before the first group of each day", () => {
    const items = chatItems([
      msg(1, at(2026, 10, 13, 10)),
      msg(2, at(2026, 10, 13, 11)),
      msg(1, at(2026, 10, 14, 9)),
    ]);
    expect(items.map((i) => (i.kind === "group" ? `group:${i.group.actor}` : i.kind === "day" ? `day:${i.at}` : "blocked"))).toEqual([
      `day:${at(2026, 10, 13, 10)}`,
      "group:1",
      "group:2",
      `day:${at(2026, 10, 14, 9)}`,
      "group:1",
    ]);
  });
});

describe("chatItems with blocked users", () => {
  it("puts a blocked user's groups in a row into one item", () => {
    const named = (name: string, h: number, min = 0) => ({ ...msg(1, at(2026, 10, 13, h, min)), actor: null, actorName: name });
    const items = chatItems(
      [named("eve", 9), named("eve", 10), named("mallory", 11), named("bob", 12), named("eve", 13)],
      (name) => name === "eve" || name === "mallory",
    );
    expect(items.map((i) => (i.kind === "blocked" ? i.groups.map((g) => g.actorName).join("+") : i.kind))).toEqual([
      "day",
      "eve+eve+mallory",
      "group",
      "eve",
    ]);
  });

  it("keeps the days apart", () => {
    const eve = (d: number) => ({ ...msg(1, at(2026, 10, d)), actor: null, actorName: "eve" });
    expect(chatItems([eve(13), eve(14)], () => true).map((i) => i.kind)).toEqual(["day", "blocked", "day", "blocked"]);
  });
});

describe("groupMessages by name", () => {
  it("does not merge different authors that share no session", () => {
    const a = { ...msg(1, at(2026, 10, 14, 9)), actor: null, actorName: "alice" };
    const b = { ...msg(1, at(2026, 10, 14, 9, 1)), actor: null, actorName: "bob" };
    expect(groupMessages([a, b])).toHaveLength(2);
  });
});

describe("chatItems with the unread line", () => {
  it("puts the line before the first unread message and starts a group there", () => {
    const t = at(2026, 10, 14, 9);
    const stored = (id: number, min: number) => ({ ...msg(1, t + min * 60_000), source: { id } }) as ChatMessage;
    const items = chatItems([stored(1, 0), stored(2, 1), stored(3, 2)], undefined, 2);
    expect(items.map((i) => (i.kind === "group" ? `group:${i.group.messages.map((m) => ("source" in m ? (m.source as { id: number }).id : 0))}` : i.kind))).toEqual([
      "day",
      "group:1",
      "new",
      "group:2,3",
    ]);
    expect(chatItems([stored(1, 0)], undefined, 9).map((i) => i.kind)).toEqual(["day", "group"]);
  });
});
