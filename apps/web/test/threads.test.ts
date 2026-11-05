import { describe, expect, it } from "vitest";
import { activeThreads, chatTarget, sidebarThreads, defaultThreadName, replyBoxText, startedIn, threadList, threadRequest, type Thread } from "../src/lib/threads";
import { initialTextState, parseServerEvent, reduceText, unreadTextIds, type TextState } from "../src/lib/textChannels";
import { mentionCounts } from "../src/lib/mentions";

const MIN = 60_000;
const thread = (id: number, patch: Partial<Thread> = {}): Thread => ({
  id,
  channel: 1,
  start: id * 10,
  name: `t${id}`,
  author: "bob",
  at: 0,
  last: 0,
  replies: 0,
  ...patch,
});

describe("thread names", () => {
  it("default to the start of the message", () => {
    expect(defaultThreadName("Shall we play tonight?")).toBe("Shall we play tonight?");
    expect(defaultThreadName("  one\n\ntwo  ")).toBe("one two");
    expect(defaultThreadName("A rather long message that goes on and on about many things")).toBe("A rather long message that goes on and…");
    expect(defaultThreadName("")).toBe("Thread");
    expect(defaultThreadName("**bold** and ||secret||")).toBe("bold and");
  });
});

describe("the reply box", () => {
  const now = 10 * 24 * 60 * MIN;
  it("counts replies and tells the last one's time", () => {
    expect(replyBoxText(thread(1), now)).toBe("No replies yet");
    expect(replyBoxText(thread(1, { replies: 1, last: now - 20_000 }), now)).toBe("1 reply · last reply just now");
    expect(replyBoxText(thread(1, { replies: 3, last: now - 5 * MIN }), now)).toBe("3 replies · last reply 5 min ago");
    expect(replyBoxText(thread(1, { replies: 3, last: now - 3 * 60 * MIN }), now)).toBe("3 replies · last reply 3 h ago");
    expect(replyBoxText(thread(1, { replies: 3, last: now - 2 * 24 * 60 * MIN }), now)).toBe("3 replies · last reply 2 d ago");
  });
});

describe("thread lists", () => {
  const threads = { 1: thread(1, { last: 5 }), 2: thread(2, { last: 9 }), 3: thread(3, { archived: true }), 4: thread(4, { channel: 2 }) };
  it("show a channel's active threads, the newest activity first", () => {
    expect(activeThreads(threads, 1).map((t) => t.id)).toEqual([2, 1]);
    expect(activeThreads(threads, 3)).toEqual([]);
  });
  it("show in the sidebar the threads we follow and the open one, as Discord", () => {
    expect(sidebarThreads(threads, 1, new Set([1]), null).map((t) => t.id)).toEqual([1]);
    expect(sidebarThreads(threads, 1, new Set([1]), 2).map((t) => t.id)).toEqual([2, 1]);
    expect(sidebarThreads(threads, 1, new Set(), null)).toEqual([]);
    // Guests cannot follow threads: they see all active ones.
    expect(sidebarThreads(threads, 1, null, null).map((t) => t.id)).toEqual([2, 1]);
  });
  it("show archived ones after the active ones, the newest first", () => {
    const archived = [thread(5, { archived: true }), thread(3, { archived: true })];
    expect(threadList(threads, archived, 1).map((t) => t.id)).toEqual([2, 1, 5, 3]);
    // One that opened again is not listed twice.
    expect(threadList({ ...threads, 5: thread(5, { last: 1 }) }, archived, 1).map((t) => t.id)).toEqual([2, 1, 5, 3]);
  });
});

describe("threads we started", () => {
  const threads = {
    1: thread(1, { authorId: 7, at: 5 }),
    2: thread(2, { authorId: 8 }),
    3: thread(3, { channel: 2, authorId: 7 }),
    4: thread(4, { author: "Guest", at: 50 }),
    5: thread(5, { author: "guest", at: 10 }),
  };
  it("are ours by id, or as guests by name since we joined, in a channel", () => {
    expect(startedIn(threads, 1, { name: "bob", userId: 7 }, 0).map((t) => t.id)).toEqual([1]);
    expect(startedIn(threads, 1, { name: "guest", userId: null }, 20).map((t) => t.id)).toEqual([4]);
    expect(startedIn(threads, 1, undefined, 0)).toEqual([]);
  });
});

describe("links to chats", () => {
  const s = {
    threads: { 5: thread(5), 6: thread(6, { channel: 3 }) },
    channels: [{ id: 1 }],
    directs: { 7: "carol" },
    voiceChannels: { 2: 8 },
  };
  it("open known threads, channels and direct chats, and ask for other chats (archived threads)", () => {
    expect(chatTarget(5, s)).toEqual({ thread: s.threads[5] });
    expect(chatTarget(1, s)).toEqual({ channel: 1 });
    expect(chatTarget(7, s)).toEqual({ direct: "carol" });
    expect(chatTarget(9, s)).toEqual({ ask: 9 });
  });
  it("go nowhere in voice chats and threads of channels we do not read", () => {
    expect(chatTarget(8, s)).toBeNull();
    expect(chatTarget(6, s)).toBeNull();
  });
});

describe("thread requests", () => {
  it("are built per action", () => {
    expect(JSON.parse(threadRequest({ get: 3 }))).toEqual({ type: "get-thread", channel: 3 });
    expect(JSON.parse(threadRequest({ create: 7, name: "Games" }))).toEqual({ type: "create-thread", message: 7, name: "Games" });
    expect(JSON.parse(threadRequest({ rename: 3, name: "x" }))).toEqual({ type: "rename-thread", channel: 3, name: "x" });
    expect(JSON.parse(threadRequest({ archive: 3, on: true }))).toEqual({ type: "archive-thread", channel: 3, on: true });
    expect(JSON.parse(threadRequest({ remove: 3 }))).toEqual({ type: "delete-thread", channel: 3 });
    expect(JSON.parse(threadRequest({ follow: 3, on: false }))).toEqual({ type: "follow-thread", channel: 3, on: false });
    expect(JSON.parse(threadRequest({ archived: 1, before: 9 }))).toEqual({ type: "threads", channel: 1, before: 9 });
    expect(JSON.parse(threadRequest({ archiveAfter: 3600 }))).toEqual({ type: "set-thread-archive", seconds: 3600 });
  });
});

describe("thread state", () => {
  const wire = (t: Thread) => JSON.stringify(t);
  const ev = (raw: string) => {
    const e = parseServerEvent(raw);
    expect(e).not.toBeNull();
    return e!;
  };
  const ready = reduceText(
    initialTextState,
    ev(`{"type":"ready","channels":[{"id":1,"name":"general"}],"threads":[${wire(thread(5))}],"follows":[5],"threadArchive":3600}`),
  );
  it("comes with ready", () => {
    expect(ready.threads[5]?.name).toBe("t5");
    expect([...ready.follows]).toEqual([5]);
    expect(ready.threadArchive).toBe(3600);
  });
  it("drops invalid threads", () => {
    expect(parseServerEvent('{"type":"thread","thread":{"id":"x"}}')).toBeNull();
  });
  it("follows thread events and follows", () => {
    let s = reduceText(ready, ev(`{"type":"thread","thread":${wire(thread(6, { replies: 2 }))}}`));
    expect(s.threads[6]?.replies).toBe(2);
    s = reduceText(s, ev('{"type":"follows","threads":[5,6]}'));
    expect([...s.follows]).toEqual([5, 6]);
    s = reduceText(s, ev('{"type":"message","message":{"id":61,"channel":6,"author":"carol","text":"hi","at":1}}'));
    s = reduceText(s, ev('{"type":"thread-deleted","channel":1,"id":6}'));
    expect(s.threads[6]).toBeUndefined();
    expect([...s.follows]).toEqual([5]);
    expect(s.messages[6]).toBeUndefined();
  });
  it("learns threads from messages, search results and archived lists", () => {
    const m = { id: 50, channel: 1, author: "bob", text: "topic", at: 1, thread: thread(5, { replies: 4 }) };
    let s = reduceText(ready, ev(JSON.stringify({ type: "history", channel: 1, messages: [m], more: false })));
    expect(s.threads[5]?.replies).toBe(4);
    expect(s.threads[5]?.quote).toEqual({ id: 50, author: "bob", text: "topic" }); // the message itself
    s = reduceText(s, ev(JSON.stringify({ type: "search-results", text: "x", messages: [], threads: [thread(8)] })));
    expect(s.threads[8]?.name).toBe("t8");
    s = reduceText(s, ev(JSON.stringify({ type: "archived-threads", channel: 1, threads: [thread(9, { archived: true })], more: true })));
    s = reduceText(s, ev(JSON.stringify({ type: "archived-threads", channel: 1, threads: [thread(7, { archived: true })], more: false })));
    expect(s.archived[1]).toEqual({ threads: [thread(9, { archived: true }), thread(7, { archived: true })], more: false });
    expect(s.threads[7]?.archived).toBe(true);
    // One archived now goes first.
    s = reduceText(s, ev(`{"type":"thread","thread":${wire(thread(5, { archived: true }))}}`));
    expect(s.archived[1]?.threads.map((t) => t.id)).toEqual([5, 9, 7]);
  });
  it("keeps archived threads of channels we still read, and drops the rest, on a channel change", () => {
    const s = reduceText(
      { ...ready, threads: { 5: thread(5), 7: thread(7, { archived: true }), 8: thread(8, { channel: 2, archived: true }) } },
      ev(`{"type":"channels","channels":[{"id":1,"name":"general"}],"threads":[]}`),
    );
    expect(Object.keys(s.threads)).toEqual(["7"]);
  });
  it("tells when a thread asked for by id is not there", () => {
    const e = ev('{"type":"no-thread","id":9}');
    expect(e).toEqual({ type: "no-thread", id: 9 });
    expect(reduceText(ready, e).notice).toEqual({ text: "You cannot open this message here", n: 1 });
  });
  it("takes the archive time", () => {
    expect(reduceText(ready, ev('{"type":"thread-archive","seconds":86400}')).threadArchive).toBe(86400);
  });
});

describe("unread threads", () => {
  const state: TextState = {
    ...initialTextState,
    channels: [{ id: 1, name: "general" }],
    threads: { 5: thread(5), 6: thread(6), 7: thread(7, { archived: true }) },
    follows: new Set([5, 7]),
    messages: {
      5: [{ id: 51, channel: 5, author: "carol", text: "@me look", at: 1 }],
      6: [{ id: 61, channel: 6, author: "carol", text: "@me look", at: 1 }],
    },
    summary: { 7: { latest: 71, mentions: 1 } },
  };
  it("are the followed active ones", () => {
    expect([...unreadTextIds(state, {})]).toEqual([5]);
  });
  it("count mentions in followed threads", () => {
    expect(mentionCounts(state, {}, "me")).toEqual({ 5: 1, 7: 1 });
  });
});
