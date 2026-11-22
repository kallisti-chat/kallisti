import { describe, expect, it } from "vitest";
import {
  highlightMentions,
  mentionCounts,
  mentions,
  mentionsMe,
  pingsMe,
  replyPing,
  suggestNames,
  mentionNames,
  keepNames,
} from "../src/lib/mentions";
import { initialTextState, type TextMessage, type TextState } from "../src/lib/textChannels";

describe("mentions", () => {
  it("finds @name as a whole name, any case", () => {
    expect(mentions("@lain hey how are you", "lain")).toBe(true);
    expect(mentions("hi @Lain!", "lain")).toBe(true);
    expect(mentions("@lainey hi", "lain")).toBe(false);
    expect(mentions("mail@lain.example", "lain")).toBe(false);
    expect(mentions("lain hi", "lain")).toBe(false);
    // Escaped text: entities end in ";", which is not a word break.
    expect(mentions("a&amp;@lain", "lain")).toBe(false);
  });

  it("tells a name here from another server's account of it", () => {
    expect(mentions("@lain@chat.example hi", "lain")).toBe(false);
    expect(mentions("@lain. hi", "lain")).toBe(true);
    expect(mentions("hi @lain@chat.example", "lain@chat.example")).toBe(true);
    expect(mentions("@lain@chat.example.org", "lain@chat.example")).toBe(false);
    expect(highlightMentions("@lain@chat.example and @lain", "me", undefined, ["lain", "lain@chat.example"])).toBe(
      '<span class="mention">@lain@chat.example</span> and <span class="mention">@lain</span>',
    );
  });

  it("never counts our own messages", () => {
    expect(mentionsMe("@lain hi", "bob", "lain")).toBe(true);
    expect(mentionsMe("@lain hi", "Lain", "lain")).toBe(false);
    expect(mentionsMe("@lain hi", null, "lain")).toBe(true); // server texts
  });

  it("works on escaped HTML and names with spaces or symbols", () => {
    expect(mentions("@lain soy, hi", "lain soy")).toBe(true);
    expect(mentions("@a&amp;b hi", "a&b")).toBe(true);
  });

  it("marks other known names as pills, mine more strongly, unknown ones not", () => {
    const known = ["pwm", "lain soy", "a&b"];
    expect(highlightMentions("btw @pwm and @Lain soy, @nobody", "lain", undefined, known)).toBe(
      'btw <span class="mention">@pwm</span> and <span class="mention">@Lain soy</span>, @nobody',
    );
    expect(highlightMentions("@a&amp;b and @lain", "lain", undefined, known)).toBe(
      '<span class="mention">@a&amp;b</span> and <span class="mention me">@lain</span>',
    );
    expect(highlightMentions("@pwmx hi", "lain", undefined, known)).toBe("@pwmx hi");
  });

  it("highlights @name outside tags only", () => {
    expect(highlightMentions("hi @Lain!", "lain")).toBe('hi <span class="mention me">@Lain</span>!');
    const link = '<a href="https://x.example/@lain">https://x.example/@lain</a>';
    expect(highlightMentions(link, "lain")).toBe(link);
  });
});

describe("mentionCounts", () => {
  const msg = (id: number, channel: number, author: string, text: string): TextMessage => ({
    id, channel, author, text, at: 0,
  });
  const state: TextState = {
    ...initialTextState,
    channels: [{ id: 1, name: "a" }],
    voiceChannels: { 9: 2 },
    messages: {
      1: [msg(1, 1, "bob", "@lain old"), msg(2, 1, "bob", "@lain new"), msg(3, 1, "lain", "@lain me myself")],
      2: [msg(4, 2, "bob", "no mention"), msg(5, 2, "carol", "@LAIN hi")],
    },
  };

  it("counts unseen mentions of me per channel, not my own", () => {
    expect(mentionCounts(state, { 1: 1 }, "lain")).toEqual({ 1: 1, 2: 1 });
    expect(mentionCounts(state, { 1: 3, 2: 5 }, "lain")).toEqual({});
  });

  it("does not count blocked users", () => {
    expect(mentionCounts(state, {}, "lain", (name) => name === "carol")).toEqual({ 1: 2 });
  });
});

describe("autocomplete", () => {
  it("suggests names that start with the text first, not me, at most 8", () => {
    const names = ["Carla", "lain", "Larry", "bob", "lain", "Alan"];
    expect(suggestNames(names, "la", "bob")).toEqual(["lain", "Larry", "Alan", "Carla"]);
    expect(suggestNames(names, "", "lain")).toEqual(["Alan", "bob", "Carla", "Larry"]);
    expect(suggestNames(Array.from({ length: 20 }, (_, i) => `u${i}`), "u", "x")).toHaveLength(8);
  });
});

describe("mentionCounts with the server's summary", () => {
  const msg = (id: number, text: string): TextMessage => ({ id, channel: 1, author: "bob", text, at: 0 });
  it("adds the summary's mentions and counts only newer messages", () => {
    const state: TextState = {
      ...initialTextState,
      channels: [{ id: 1, name: "a" }],
      summary: { 1: { latest: 5, mentions: 2 }, 3: { latest: 1, mentions: 4 } }, // 3 was deleted
      messages: { 1: [msg(4, "@lain counted in the summary"), msg(6, "@lain new")] },
    };
    expect(mentionCounts(state, { 1: 3 }, "lain")).toEqual({ 1: 3 });
    // Read past the summary: only what is newer than what was read.
    expect(mentionCounts(state, { 1: 5 }, "lain")).toEqual({ 1: 1 });
    expect(mentionCounts(state, { 1: 6 }, "lain")).toEqual({});
  });
});

describe("replies ping", () => {
  it("counts a reply to me like a mention", () => {
    expect(pingsMe("no at here", "bob", "lain", "Lain")).toBe(true);
    expect(pingsMe("no at here", "bob", "carol", "lain")).toBe(false);
    expect(pingsMe("@lain", "bob", undefined, "lain")).toBe(true);
    expect(pingsMe("reply to myself", "lain", "lain", "lain")).toBe(false);
  });

  it("is in the counts", () => {
    const state: TextState = {
      ...initialTextState,
      channels: [{ id: 1, name: "a" }],
      messages: { 1: [{ id: 3, channel: 1, author: "bob", text: "sure", at: 0, reply: { id: 2, author: "lain", text: "q" } }] },
    };
    expect(mentionCounts(state, {}, "lain")).toEqual({ 1: 1 });
  });

  it("not when the reply has no ping", () => {
    const quiet: TextMessage = { id: 3, channel: 1, author: "bob", text: "sure", at: 0, reply: { id: 2, author: "lain", text: "q" }, noPing: true };
    expect(replyPing(quiet)).toBeUndefined();
    expect(replyPing({ ...quiet, noPing: undefined })).toBe("lain");
    // A webhook's post may carry a user's name: answering it pings nobody.
    const hook: TextMessage = { ...quiet, noPing: undefined, reply: { id: 2, author: "lain", text: "q", webhook: true } };
    expect(replyPing(hook)).toBeUndefined();
    expect(mentionCounts({ ...initialTextState, channels: [{ id: 1, name: "a" }], messages: { 1: [quiet] } }, {}, "lain")).toEqual({});
  });
});

describe("@everyone and @here (flagged by the server)", () => {
  it("ping all but the author", () => {
    expect(pingsMe("@everyone hi", "mod", undefined, "lain", "everyone")).toBe(true);
    expect(pingsMe("@here hi", "mod", undefined, "lain", "here")).toBe(true);
    expect(pingsMe("@everyone hi", "lain", undefined, "lain", "everyone")).toBe(false);
    expect(pingsMe("@everyone hi", "bob", undefined, "lain")).toBe(false); // not a moderator: plain text
  });

  it("are highlighted when flagged", () => {
    expect(highlightMentions("@everyone hi", "lain", "everyone")).toBe('<span class="mention">@everyone</span> hi');
    expect(highlightMentions("@everyone hi", "lain")).toBe("@everyone hi");
  });

  it("are in the counts", () => {
    const state: TextState = {
      ...initialTextState,
      channels: [{ id: 1, name: "a" }],
      messages: { 1: [{ id: 3, channel: 1, author: "mod", text: "@here now", at: 0, mentionAll: "here" }] },
    };
    expect(mentionCounts(state, {}, "lain")).toEqual({ 1: 1 });
  });
});

describe("mentionNames", () => {
  it("offers who is online, who wrote here, and registered users who are offline", () => {
    const names = mentionNames({ online: ["lain"], authors: ["bob"], members: ["carla", "lain"], all: false });
    expect(names).toEqual(expect.arrayContaining(["lain", "bob", "carla"]));
    expect(suggestNames(names, "car", "lain")).toEqual(["carla"]);
  });

  it("adds everyone and here when allowed", () => {
    expect(mentionNames({ online: [], authors: [], members: [], all: true })).toEqual(["everyone", "here"]);
  });

  it("gives pills to the names it suggests, e.g. an offline guest who wrote here", () => {
    const known = mentionNames({ online: [], authors: ["Ergo-GrapheneOS"], members: [], all: false });
    expect(highlightMentions("@Ergo-GrapheneOS apk is up", "lain", undefined, known)).toBe('<span class="mention">@Ergo-GrapheneOS</span> apk is up');
  });
});


describe("keepNames", () => {
  it("keeps the list before when the names are the same", () => {
    const keep = keepNames();
    const first = keep(["ada", "bob"]);
    expect(keep(["ada", "bob"])).toBe(first);
    const next = keep(["ada", "cy"]);
    expect(next).toEqual(["ada", "cy"]);
    expect(keep(["ada", "cy"])).toBe(next);
  });
});
