import { webhookPicture } from "../src/lib/webhooks";
import { describe, expect, it } from "vitest";
import {
  deleteMessageRequest,
  editMessageRequest,
  historyRequest,
  initialTextState,
  markReadRequest,
  markUnreadRequest,
  linkify,
  pageAnswered,
  manageRequest,
  parseServerEvent,
  reduceText,
  searchRequest,
  sendRequest,
  webhookPictureTokenRequest,
  sendAnswer,
  toChatMessage,
  unreadTextIds,
  autoSeen,
  resyncRequest,
  uploadTokenRequest,
  fileKey,
  voiceChatRequest,
  httpBase,
  formatSize,
  type TextMessage,
} from "../src/lib/textChannels";
import { mentionCounts } from "../src/lib/mentions";

const msg = (id: number, channel = 1, author = "alice"): TextMessage => ({
  id,
  channel,
  author,
  text: `m${id}`,
  at: id * 1000,
});

describe("parseServerEvent", () => {
  it("keeps a rate limit error of a background request out of the error line", () => {
    const ev = parseServerEvent('{"type":"error","message":"too many requests, slow down","about":"rate"}');
    expect(ev).toEqual({ type: "error", message: "too many requests, slow down", about: "rate" });
    expect(reduceText(initialTextState, ev!)).toMatchObject({ lastError: "too many requests, slow down", quietError: true });
  });
  it("keeps the text of a refused send for the composer", () => {
    const ev = parseServerEvent('{"type":"error","message":"no","about":"refused","channel":3,"text":"buy spam"}');
    expect(ev).toMatchObject({ returned: { channel: 3, text: "buy spam" } });
    const slow = parseServerEvent('{"type":"error","message":"wait","about":"slow-mode","channel":3,"until":5,"text":"hi"}');
    expect(slow).toMatchObject({ slow: { channel: 3, until: 5 }, returned: { channel: 3, text: "hi" } });
    const s1 = reduceText(initialTextState, ev!);
    expect(s1.returned).toEqual({ channel: 3, text: "buy spam", n: 1 });
    expect(reduceText(s1, ev!).returned?.n).toBe(2);
  });
  it("reads the upload limit of ready", () => {
    const ev = parseServerEvent('{"type":"ready","channels":[],"uploadMax":1048576}');
    expect(ev).toMatchObject({ uploadMax: 1048576 });
    expect(reduceText(initialTextState, ev!).uploadMax).toBe(1048576);
    expect(parseServerEvent('{"type":"ready","channels":[],"uploadMax":"x"}')).not.toHaveProperty("uploadMax");
  });
  it("reads from ready whether avatars are served over HTTP", () => {
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[],"avatars":true}')!).avatars).toBe(true);
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[],"avatars":1}')!).avatars).toBe(false);
  });
  it("accepts known events", () => {
    expect(parseServerEvent('{"type":"ready","channels":[{"id":1,"name":"general"}]}')).toEqual({
      type: "ready",
      channels: [{ id: 1, name: "general" }],
    });
  });

  it("rejects broken or unknown input", () => {
    expect(parseServerEvent("nope")).toBeNull();
    expect(parseServerEvent('{"type":"dance"}')).toBeNull();
    expect(parseServerEvent('{"type":"ready","channels":"x"}')).toBeNull();
    expect(parseServerEvent('{"type":"message","message":{"id":"1"}}')).toBeNull();
  });
});

describe("reduceText", () => {
  const ready = reduceText(initialTextState, {
    type: "ready",
    channels: [{ id: 1, name: "general" }],
  });

  it("is unavailable until ready", () => {
    expect(initialTextState.available).toBe(false);
    expect(ready.available).toBe(true);
    expect(ready.channels).toEqual([{ id: 1, name: "general" }]);
  });

  it("merges history pages and live messages in id order without duplicates", () => {
    let s = reduceText(ready, { type: "message", message: msg(5) });
    s = reduceText(s, { type: "history", channel: 1, messages: [msg(3), msg(4), msg(5)], more: true });
    s = reduceText(s, { type: "history", channel: 1, messages: [msg(1), msg(2)], more: false });
    s = reduceText(s, { type: "message", message: msg(6) });
    expect(s.messages[1]?.map((m) => m.id)).toEqual([1, 2, 3, 4, 5, 6]);
    expect(s.loaded[1]).toBe(true);
    expect(s.more[1]).toBe(false);
  });

  it("keeps channels apart", () => {
    const s = reduceText(ready, { type: "message", message: msg(1, 2) });
    expect(s.messages[1]).toBeUndefined();
    expect(s.messages[2]).toHaveLength(1);
  });

  it("stores the last error", () => {
    expect(reduceText(ready, { type: "error", message: "nope" }).lastError).toBe("nope");
  });

  it("shows errors about channel changes as a notice, not in the chat", () => {
    const failed = reduceText(ready, { type: "error", message: "name in use", about: "manage" });
    expect(failed.lastError).toBeNull();
    expect(failed.notice).toEqual({ text: "name in use", n: 1 });
    expect(reduceText(failed, { type: "error", message: "x", about: "manage" }).notice?.n).toBe(2);
  });

  it("shows a notice from the server (a problem of our OBS stream)", () => {
    const ev = parseServerEvent(JSON.stringify({ type: "notice", message: "B-frames" }));
    expect(ev).toEqual({ type: "notice", message: "B-frames" });
    expect(parseServerEvent(JSON.stringify({ type: "notice" }))).toBeNull();
    const s = reduceText(ready, ev!);
    expect(s.notice).toEqual({ text: "B-frames", n: 1 });
    expect(s.lastError).toBeNull();
  });

  it("reads the topic of an error", () => {
    expect(parseServerEvent('{"type":"error","message":"m","about":"manage"}')).toEqual({ type: "error", message: "m", about: "manage" });
    expect(parseServerEvent('{"type":"error","message":"m","about":"other"}')).toEqual({ type: "error", message: "m" });
  });
});

describe("pageAnswered", () => {
  const ready = reduceText(initialTextState, { type: "ready", channels: [{ id: 1, name: "general" }] });
  const first = reduceText(ready, { type: "history", channel: 1, messages: [msg(10), msg(11)], more: true });
  const page = { channel: 1, before: 10 };

  it("stays pending on unrelated events", () => {
    let s = reduceText(first, { type: "message", message: msg(12) });
    s = reduceText(s, { type: "history", channel: 2, messages: [msg(1, 2)], more: false });
    s = reduceText(s, { type: "messages-deleted", channel: 1, ids: [11] });
    expect(pageAnswered(s, page)).toBe(false);
  });

  it("is answered by older messages of the channel", () => {
    const s = reduceText(first, { type: "history", channel: 1, messages: [msg(8), msg(9)], more: true });
    expect(pageAnswered(s, page)).toBe(true);
  });

  it("is answered by an empty last page", () => {
    const s = reduceText(first, { type: "history", channel: 1, messages: [], more: false });
    expect(pageAnswered(s, page)).toBe(true);
  });

  it("ends when a resync drops the history", () => {
    expect(pageAnswered(reduceText(first, { type: "resync" }), page)).toBe(true);
  });
});

describe("requests", () => {
  it("builds history and send requests", () => {
    expect(JSON.parse(historyRequest(1))).toEqual({ type: "history", channel: 1 });
    expect(JSON.parse(historyRequest(1, 40))).toEqual({ type: "history", channel: 1, before: 40 });
    expect(JSON.parse(sendRequest(2, "hi"))).toEqual({ type: "send", channel: 2, text: "hi" });
    expect(JSON.parse(sendRequest(2, "yes", undefined, 7))).toEqual({ type: "send", channel: 2, text: "yes", reply: 7 });
    expect(JSON.parse(sendRequest(2, "yes", undefined, 7, true))).toEqual({ type: "send", channel: 2, text: "yes", reply: 7, noPing: true });
    expect(JSON.parse(sendRequest(2, "no reply", undefined, undefined, true))).toEqual({ type: "send", channel: 2, text: "no reply" });
    expect(JSON.parse(sendRequest(2, "", [{ key: "k1" }], undefined, undefined, "n1"))).toEqual({ type: "send", channel: 2, text: "", upload: "k1", nonce: "n1" });
  });
});

describe("sendAnswer", () => {
  const m = { id: 1, channel: 2, author: "me", text: "", at: 0 };
  it("reads the nonce of our own message", () => {
    expect(parseServerEvent(JSON.stringify({ type: "message", message: m, nonce: "n1" }))).toEqual({ type: "message", message: m, nonce: "n1" });
    expect(parseServerEvent(JSON.stringify({ type: "message", message: m, nonce: 5 }))).toEqual({ type: "message", message: m });
  });
  it("reads the nonce of a refused send", () => {
    const slow = { type: "error", message: "wait", about: "slow-mode", channel: 3, until: 5, text: "hi", nonce: "n1" };
    expect(parseServerEvent(JSON.stringify(slow))).toMatchObject({ slow: { channel: 3, until: 5 }, returned: { channel: 3, text: "hi" }, nonce: "n1" });
    expect(parseServerEvent(JSON.stringify({ type: "error", message: "no", about: "refused", nonce: "n1" }))).toEqual({ type: "error", message: "no", nonce: "n1" });
    expect(parseServerEvent(JSON.stringify({ type: "error", message: "unknown upload", nonce: "n1" }))).toEqual({ type: "error", message: "unknown upload", nonce: "n1" });
    expect(parseServerEvent(JSON.stringify({ type: "error", message: "x", nonce: 1 }))).toEqual({ type: "error", message: "x" });
  });
  it("confirms by the message with the nonce, refuses by an error with it, ignores the rest", () => {
    expect(sendAnswer({ type: "message", message: m, nonce: "n1" }, "n1")).toEqual({});
    expect(sendAnswer({ type: "message", message: m, nonce: "n2" }, "n1")).toBeNull();
    expect(sendAnswer({ type: "message", message: m }, "n1")).toBeNull();
    // A server without nonces: our upload's message.
    const withFile = (url: string) => ({ ...m, upload: { url, name: "a.png", type: "image/png", size: 1 } });
    expect(sendAnswer({ type: "message", message: withFile("/files/k1/a.png") }, "n1", "k1")).toEqual({});
    expect(sendAnswer({ type: "message", message: withFile("/files/k10/a.png") }, "n1", "k1")).toBeNull();
    expect(sendAnswer({ type: "message", message: withFile("/files/k1/a.png"), nonce: "n2" }, "n1", "k1")).toBeNull();
    expect(sendAnswer({ type: "error", message: "Slow mode", slow: { channel: 2, until: 9 }, nonce: "n1" }, "n1")).toEqual({ error: "Slow mode" });
    expect(sendAnswer({ type: "error", message: "unknown upload", nonce: "n1" }, "n1")).toEqual({ error: "unknown upload" });
    // Errors of other requests, or of other sends, are not the answer
    // (servers without nonces: the send times out).
    expect(sendAnswer({ type: "error", message: "unknown upload" }, "n1")).toBeNull();
    expect(sendAnswer({ type: "error", message: "too many requests", about: "rate" }, "n1")).toBeNull();
    expect(sendAnswer({ type: "error", message: "Slow mode", nonce: "n2" }, "n1")).toBeNull();
    expect(sendAnswer({ type: "resync" }, "n1")).toBeNull();
  });
});

describe("linkify", () => {
  it("turns URLs in escaped text into links", () => {
    expect(linkify("see https://example.org/a?b=1&amp;c=2 now")).toBe(
      'see <a href="https://example.org/a?b=1&amp;c=2">https://example.org/a?b=1&amp;c=2</a> now',
    );
  });

  it("leaves a trailing dot or bracket outside the link", () => {
    expect(linkify("(https://example.org).")).toBe(
      '(<a href="https://example.org">https://example.org</a>).',
    );
  });

  it("ignores other schemes", () => {
    expect(linkify("javascript:alert(1)")).toBe("javascript:alert(1)");
  });
});

describe("toChatMessage", () => {
  it("escapes the plain text and keeps the author", () => {
    const m = toChatMessage({ ...msg(1), text: "<b>hi</b> https://x.org" });
    expect(m).toMatchObject({
      actor: null,
      actorName: "alice",
      text: '&lt;b&gt;hi&lt;/b&gt; <a href="https://x.org">https://x.org</a>',
      at: 1000,
    });
  });
});

describe("toChatMessage cache", () => {
  it("returns the same object for the same message", () => {
    const m = msg(1);
    expect(toChatMessage(m)).toBe(toChatMessage(m));
  });
});

describe("unreadTextIds", () => {
  it("lists channels with messages newer than the last seen one", () => {
    let s = reduceText(initialTextState, {
      type: "ready",
      channels: [
        { id: 1, name: "a" },
        { id: 2, name: "b" },
        { id: 3, name: "c" },
      ],
    });
    s = reduceText(s, { type: "message", message: msg(5, 1) });
    s = reduceText(s, { type: "message", message: msg(6, 2) });
    expect(unreadTextIds(s, { 1: 5 })).toEqual(new Set([2]));
  });
});

describe("autoSeen", () => {
  let s = reduceText(initialTextState, { type: "message", message: msg(5, 1) });
  s = reduceText(s, { type: "message", message: msg(6, 2) });

  it("marks new messages in the open chats seen", () => {
    expect(autoSeen(s, [1, 2], { 1: 5 }, true)).toEqual([[2, 6]]);
  });

  it("marks nothing in a hidden tab (another tab may be the one the user reads)", () => {
    expect(autoSeen(s, [1, 2], {}, false)).toEqual([]);
  });
});

describe("uploads", () => {
  it("parses messages with an attachment", () => {
    const ev = parseServerEvent(
      JSON.stringify({
        type: "message",
        message: { ...msg(1), upload: { url: "/files/k/cat.png", name: "cat.png", type: "image/png", size: 3 } },
      }),
    );
    expect(ev?.type === "message" && ev.message.upload?.name).toBe("cat.png");
  });

  it("keeps an attachment's size and length", () => {
    const upload = { url: "/files/k/a.mp4", name: "a.mp4", type: "video/mp4", size: 3, width: 1280, height: 720, duration: 2500 };
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), upload } }));
    expect(ev?.type === "message" && ev.message.upload).toEqual(upload);
    expect(toChatMessage({ ...msg(2), upload }, "http://srv").uploads?.[0]).toMatchObject({ width: 1280, height: 720, duration: 2500 });
    const broken = { ...upload, width: "wide" };
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), upload: broken } }))).toBeNull();
  });

  it("drops a broken attachment", () => {
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), upload: { url: 1 } } }))).toBeNull();
  });

  it("parses the upload token event", () => {
    expect(parseServerEvent('{"type":"upload-token","token":"abc"}')).toEqual({ type: "upload-token", token: "abc" });
  });

  it("sends an upload key with a message", () => {
    expect(JSON.parse(sendRequest(1, "", [{ key: "key1" }]))).toEqual({ type: "send", channel: 1, text: "", upload: "key1" });
    expect(JSON.parse(uploadTokenRequest())).toEqual({ type: "upload-token" });
  });

  it("puts the attachment with an absolute URL on the chat message", () => {
    const m = toChatMessage(
      { ...msg(9), upload: { url: "/files/k/cat.png", name: "cat.png", type: "image/png", size: 3 } },
      "http://srv:8080",
    );
    expect(m.uploads).toEqual([{ url: "http://srv:8080/files/k/cat.png", name: "cat.png", type: "image/png", size: 3 }]);
  });

  it("keeps a preview on our server", () => {
    const upload = { url: "/files/k/a.mp4", name: "a.mp4", type: "video/mp4", size: 3, preview: "/files/k/a.mp4?preview" };
    const ev = parseServerEvent(JSON.stringify({ type: "message-edited", message: { ...msg(1), upload } }));
    expect(ev?.type === "message-edited" && ev.message.upload?.preview).toBe("/files/k/a.mp4?preview");
    expect(toChatMessage({ ...msg(3), upload }, "http://srv").uploads?.[0]?.preview).toBe("http://srv/files/k/a.mp4?preview");
    // Elsewhere: dropped, the attachment stays.
    const away = toChatMessage({ ...msg(4), upload: { ...upload, preview: "https://evil.example/files/x" } }, "http://srv").uploads?.[0];
    expect(away?.url).toBe("http://srv/files/k/a.mp4");
    expect(away && "preview" in away).toBe(false);
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), upload: { ...upload, preview: 1 } } }))).toBeNull();
  });
});

describe("several files", () => {
  const file = (k: string, extra = {}) => ({ url: `/files/${k}/${k}.png`, name: `${k}.png`, type: "image/png", size: 1, ...extra });
  it("parses the list, with descriptions", () => {
    const uploads = [file("a", { description: "a cat" }), file("b")];
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), uploads, upload: uploads[0] } }));
    expect(ev?.type === "message" && ev.message.uploads).toEqual(uploads);
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), uploads: [file("a"), { url: 1 }] } }))).toBeNull();
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), uploads: [file("a", { description: 1 })] } }))).toBeNull();
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), uploads: file("a") } }))).toBeNull();
  });
  it("shows the list, or the one upload of an older server, with spoilers", () => {
    const shown = toChatMessage({ ...msg(1), uploads: [file("a"), file("SPOILER_b")], upload: file("a") }, "http://srv").uploads;
    expect(shown?.map((u) => [u.url, u.name, u.spoiler])).toEqual([
      ["http://srv/files/a/a.png", "a.png", undefined],
      ["http://srv/files/SPOILER_b/SPOILER_b.png", "b.png", true],
    ]);
    expect(toChatMessage({ ...msg(2), upload: file("a") }, "http://srv").uploads?.map((u) => u.name)).toEqual(["a.png"]);
    expect(toChatMessage(msg(3), "http://srv")).not.toHaveProperty("uploads");
  });
  it("sends one file in the old form, more (or a description) as a list", () => {
    expect(JSON.parse(sendRequest(1, "", [{ key: "a" }]))).toEqual({ type: "send", channel: 1, text: "", upload: "a" });
    expect(JSON.parse(sendRequest(1, "", [{ key: "a", description: "a cat" }]))).toEqual({ type: "send", channel: 1, text: "", uploads: [{ key: "a", description: "a cat" }] });
    expect(JSON.parse(sendRequest(1, "x", [{ key: "a" }, { key: "b" }]))).toEqual({ type: "send", channel: 1, text: "x", uploads: [{ key: "a" }, { key: "b" }] });
    expect(JSON.parse(sendRequest(1, "x", []))).toEqual({ type: "send", channel: 1, text: "x" });
  });
  it("reads how many files the server takes (older servers: one)", () => {
    const ev = parseServerEvent('{"type":"ready","channels":[],"maxUploads":10}');
    expect(reduceText(initialTextState, ev!).maxUploads).toBe(10);
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[]}')!).maxUploads).toBe(1);
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[],"maxUploads":0}')!).maxUploads).toBe(1);
    // Polls too, only where the server says so.
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[],"polls":true}')!).polls).toBe(true);
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[]}')!).polls).toBe(false);
  });
  it("asks for the tokens of several files in one request", () => {
    expect(JSON.parse(uploadTokenRequest(3))).toEqual({ type: "upload-token", count: 3 });
    expect(JSON.parse(uploadTokenRequest(1))).toEqual({ type: "upload-token" });
    expect(parseServerEvent('{"type":"upload-token","token":"a","tokens":["a","b"]}')).toEqual({ type: "upload-token", token: "a", tokens: ["a", "b"] });
    expect(parseServerEvent('{"type":"upload-token","token":"a","tokens":["a",1]}')).toEqual({ type: "upload-token", token: "a" });
  });
  it("finds a file's key in its URL", () => {
    expect(fileKey("http://srv/files/k1/a%20b.png")).toBe("k1");
    expect(fileKey("/files/k2/x.png?preview")).toBe("k2");
    expect(fileKey("http://srv/api/upload")).toBeNull();
  });
  it("confirms a send without nonce by any of its files", () => {
    const m = { id: 1, channel: 2, author: "me", text: "", at: 0, uploads: [file("k0"), file("k1")] };
    expect(sendAnswer({ type: "message", message: m }, "n1", "k0")).toEqual({});
  });
});

describe("polls", () => {
  const poll = { question: "Pizza?", answers: [{ text: "Yes", votes: 0 }, { text: "No", votes: 0 }], ends: 99, voters: 0 };
  it("parses poll messages and drops broken polls", () => {
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), poll } }));
    expect(ev?.type === "message" && ev.message.poll).toEqual(poll);
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), poll: { question: 1 } } }))).toBeNull();
  });
  it("puts a poll event on the message, in pins and search too, keeping our votes", () => {
    const mine = { ...poll, mine: [0], voters: 1 };
    let s = reduceText(initialTextState, { type: "history", channel: 1, messages: [{ ...msg(1), poll: mine }], more: false });
    s = { ...s, pins: { 1: [{ ...msg(1), poll: mine }] }, search: { text: "", messages: [{ ...msg(1), poll: mine }] } };
    const ev = parseServerEvent(JSON.stringify({ type: "poll", channel: 1, message: 1, poll: { ...poll, voters: 2 } }));
    expect(ev).toMatchObject({ type: "poll", channel: 1, message: 1 });
    s = reduceText(s, ev!);
    for (const m of [s.messages[1]![0], s.pins[1]![0], s.search!.messages[0]]) expect(m?.poll).toMatchObject({ voters: 2, mine: [0] });
    expect(reduceText(s, { type: "poll", channel: 1, message: 1, poll: { ...poll, mine: [] } }).messages[1]![0]!.poll?.mine).toEqual([]);
    expect(parseServerEvent(JSON.stringify({ type: "poll", channel: 1, message: 1, poll: {} }))).toBeNull();
  });
  it("keeps our votes when a pinned or edited poll message comes", () => {
    let s = reduceText(initialTextState, { type: "history", channel: 1, messages: [{ ...msg(1), poll: { ...poll, mine: [1] } }], more: false });
    s = reduceText(s, { type: "message-pinned", message: { ...msg(1), pinned: 5, poll } });
    expect(s.messages[1]![0]!.poll?.mine).toEqual([1]);
    s = reduceText(s, { type: "message-edited", message: { ...msg(1), poll } });
    expect(s.messages[1]![0]!.poll?.mine).toEqual([1]);
  });
  it("keeps our votes in the pin list, and an ended poll ended after a late reply", () => {
    let s = reduceText(initialTextState, { type: "history", channel: 1, messages: [{ ...msg(1), poll: { ...poll, mine: [1], ended: 5 } }], more: false });
    s = { ...s, pins: { 1: [] } };
    s = reduceText(s, { type: "message-pinned", message: { ...msg(1), pinned: 5, poll: { ...poll, ended: 5 } } });
    expect(s.pins[1]![0]!.poll?.mine).toEqual([1]);
    s = reduceText(s, { type: "history", channel: 1, messages: [{ ...msg(1), poll: { ...poll, mine: [1] } }], more: false });
    expect(s.messages[1]![0]!.poll?.ended).toBe(5);
  });
  it("sends a poll", () => {
    const body = { question: "Q", answers: [{ text: "a" }, { text: "b" }], multiple: false, anonymous: true, hours: 1 };
    expect(JSON.parse(sendRequest(1, "", [], undefined, undefined, "n", undefined, body))).toEqual({ type: "send", channel: 1, text: "", nonce: "n", poll: body });
  });
});

describe("httpBase", () => {
  it("turns the WebSocket URL into the server's HTTP origin", () => {
    expect(httpBase("ws://localhost:18081")).toBe("http://localhost:18081");
    expect(httpBase("wss://chat.example/ws")).toBe("https://chat.example");
  });
});

describe("formatSize", () => {
  it("writes readable sizes", () => {
    expect(formatSize(512)).toBe("512 B");
    expect(formatSize(1536)).toBe("1.5 KB");
    expect(formatSize(1258291)).toBe("1.2 MB");
  });
});

describe("attachment URL check", () => {
  const withUrl = (url: string) => toChatMessage({ ...msg(Math.random() * 1e9), upload: { url, name: "x", type: "image/png", size: 1 } }, "https://srv.example");

  it("keeps files on our server", () => {
    expect(withUrl("/files/k/x.png").uploads?.[0]?.url).toBe("https://srv.example/files/k/x.png");
  });

  it("drops URLs that leave the server or the files path", () => {
    expect(withUrl("@evil.example/x").uploads).toBeUndefined();
    expect(withUrl(".evil.example/x").uploads).toBeUndefined();
    expect(withUrl("//evil.example/files/x").uploads).toBeUndefined();
    expect(withUrl("javascript:alert(1)").uploads).toBeUndefined();
    expect(withUrl("/api/upload").uploads).toBeUndefined();
  });
});

describe("robustness", () => {
  const ready = reduceText(initialTextState, { type: "ready", channels: [{ id: 1, name: "general" }] });

  it("parses resync and verified authors", () => {
    expect(parseServerEvent('{"type":"resync"}')).toEqual({ type: "resync" });
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), verified: true } }));
    expect(ev?.type === "message" && ev.message.verified).toBe(true);
  });

  it("forgets loaded history on resync and counts the resyncs", () => {
    let s = reduceText(ready, { type: "history", channel: 1, messages: [msg(1)], more: false });
    s = reduceText(s, { type: "resync" });
    expect(s.messages).toEqual({});
    expect(s.loaded).toEqual({});
    expect(s.resyncs).toBe(1);
    expect(s.channels).toEqual(ready.channels);
  });

  it("asks for the whole state again after a resync", () => {
    expect(JSON.parse(resyncRequest)).toEqual({ type: "resync" });
  });

  it("takes the state sent again after a resync, and keeps the archived threads it knows", () => {
    const thread = (id: number, archived = false, channel = 1) => ({ id, channel, start: id, name: "t", author: "a", at: 0, last: 0, replies: 0, archived });
    const channels = [{ id: 1, name: "a" }];
    let s = reduceText(initialTextState, { type: "ready", channels, threads: [thread(1), thread(2)], follows: [1], reads: { 1: 3 } });
    s = reduceText(s, { type: "thread", thread: thread(3, true) });
    s = reduceText(s, { type: "thread", thread: thread(4, true, 2) }); // of a channel we no longer see
    s = reduceText(s, { type: "resync" });
    // Missed meanwhile: thread 2 was deleted, thread 1 unfollowed, chat 1 read further.
    s = reduceText(s, { type: "ready", channels, threads: [thread(1)], follows: [], reads: { 1: 8 } });
    expect(Object.keys(s.threads).map(Number)).toEqual([1, 3]);
    expect(s.follows).toEqual(new Set());
    expect(s.reads).toEqual({ 1: 8 });
  });

  it("marks verified authors on chat messages", () => {
    expect(toChatMessage({ ...msg(77), verified: true }).verified).toBe(true);
    expect(toChatMessage(msg(78)).verified).toBe(false);
  });
});

describe("voice channel chats", () => {
  it("knows whether the server stores voice channel chats", () => {
    const on = reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[],"voiceChats":true}')!);
    const off = reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[]}')!);
    expect(on.voiceChats).toBe(true);
    expect(off.voiceChats).toBe(false);
  });

  it("maps a voice channel to the channel that holds its chat", () => {
    const ev = parseServerEvent('{"type":"voice-chat","voice":5,"channel":42}');
    expect(ev).toEqual({ type: "voice-chat", voice: 5, channel: 42 });
    expect(reduceText(initialTextState, ev!).voiceChannels[5]).toBe(42);
    expect(parseServerEvent('{"type":"voice-chat","voice":"5","channel":42}')).toBeNull();
  });

  it("asks for a voice channel's chat", () => {
    expect(JSON.parse(voiceChatRequest(5))).toEqual({ type: "voice-chat", voice: 5 });
  });
});

describe("managing text channels", () => {
  it("takes a new channel list", () => {
    const ev = parseServerEvent('{"type":"channels","channels":[{"id":2,"name":"games"}]}');
    expect(ev).toEqual({ type: "channels", channels: [{ id: 2, name: "games" }] });
    expect(reduceText(initialTextState, ev!).channels).toEqual([{ id: 2, name: "games" }]);
  });

  it("makes the requests", () => {
    expect(JSON.parse(manageRequest({ create: "games" }))).toEqual({ type: "create-channel", name: "games" });
    expect(JSON.parse(manageRequest({ rename: 2, name: "x" }))).toEqual({ type: "rename-channel", channel: 2, name: "x" });
    expect(JSON.parse(manageRequest({ remove: 2 }))).toEqual({ type: "delete-channel", channel: 2 });
    expect(JSON.parse(manageRequest({ order: [3, 2] }))).toEqual({ type: "order-channels", channels: [3, 2] });
    expect(JSON.parse(manageRequest({ moderator: 5, on: true }))).toEqual({ type: "set-moderator", user: 5, moderator: true });
    expect(JSON.parse(manageRequest({ topic: 2, text: "hi" }))).toEqual({ type: "set-topic", channel: 2, text: "hi" });
    expect(JSON.parse(manageRequest({ ban: 4, reason: "spam", seconds: 3600 }))).toEqual({ type: "ban", session: 4, text: "spam", seconds: 3600 });
    expect(JSON.parse(manageRequest({ timeout: 4, seconds: 300 }))).toEqual({ type: "time-out", session: 4, seconds: 300 });
  });

  it("reads topics", () => {
    const ev = parseServerEvent('{"type":"channels","channels":[{"id":2,"name":"games","topic":"Play"}]}');
    expect(ev).toEqual({ type: "channels", channels: [{ id: 2, name: "games", topic: "Play" }] });
  });
});

describe("roles", () => {
  it("come with ready and change later", () => {
    const ready = parseServerEvent('{"type":"ready","channels":[],"voiceChats":true,"roles":{"admins":[3],"moderators":[]}}');
    const s = reduceText(initialTextState, ready!);
    expect(s.roles).toEqual({ admins: [3], moderators: [] });
    const changed = parseServerEvent('{"type":"roles","admins":[3],"moderators":[5]}');
    expect(reduceText(s, changed!).roles).toEqual({ admins: [3], moderators: [5] });
    expect(parseServerEvent('{"type":"roles","admins":"x","moderators":[]}')).toBeNull();
  });

});

describe("time-outs", () => {
  it("come with ready and change later", () => {
    const ready = parseServerEvent('{"type":"ready","channels":[],"voiceChats":true,"timeouts":[{"user":3,"until":5000}]}');
    const s = reduceText(initialTextState, ready!);
    expect(s.timeouts).toEqual([{ user: 3, until: 5000 }]);
    const changed = parseServerEvent('{"type":"timeouts","timeouts":[{"session":7,"until":6000}]}');
    expect(reduceText(s, changed!).timeouts).toEqual([{ session: 7, until: 6000 }]);
    expect(reduceText(s, parseServerEvent('{"type":"ready","channels":[]}')!).timeouts).toEqual([]);
  });

  it("drops broken lists", () => {
    expect(parseServerEvent('{"type":"timeouts","timeouts":[{"until":"x"}]}')).toBeNull();
    expect(parseServerEvent('{"type":"timeouts","timeouts":[{"until":1}]}')).toBeNull(); // nobody
    expect(parseServerEvent('{"type":"timeouts"}')).toBeNull();
  });
});

describe("edited and deleted messages", () => {
  const ready = reduceText(initialTextState, { type: "ready", channels: [{ id: 1, name: "general" }] });
  it("replaces and removes them", () => {
    let s = reduceText(ready, { type: "message", message: msg(1) });
    s = reduceText(s, { type: "message", message: msg(2) });
    const edited = parseServerEvent(JSON.stringify({ type: "message-edited", message: { ...msg(1), text: "new", edited: 5 } }));
    s = reduceText(s, edited!);
    expect(s.messages[1]?.map((m) => [m.id, m.text, m.edited])).toEqual([
      [1, "new", 5],
      [2, msg(2).text, undefined],
    ]);
    const deleted = parseServerEvent('{"type":"message-deleted","channel":1,"id":2}');
    expect(deleted).toEqual({ type: "messages-deleted", channel: 1, ids: [2] });
    expect(reduceText(s, deleted!).messages[1]?.map((m) => m.id)).toEqual([1]);
  });

  it("drops many at once (a user's messages deleted by staff)", () => {
    let s = reduceText(initialTextState, { type: "history", channel: 1, messages: [msg(1), msg(2), msg(3)], more: false });
    s = reduceText(s, { type: "pins", channel: 1, messages: [msg(3)] });
    s = reduceText(s, { type: "search-results", text: "x", messages: [msg(1), msg(3)] });
    const deleted = parseServerEvent('{"type":"messages-deleted","channel":1,"ids":[1,3]}');
    expect(deleted).toEqual({ type: "messages-deleted", channel: 1, ids: [1, 3] });
    s = reduceText(s, deleted!);
    expect([s.messages[1]?.map((m) => m.id), s.pins[1], s.search?.messages]).toEqual([[2], [], []]);
    expect(parseServerEvent('{"type":"messages-deleted","channel":1,"ids":[1,"x"]}')).toBeNull();
  });

  it("builds the requests", () => {
    expect(JSON.parse(editMessageRequest(4, "x"))).toEqual({ type: "edit-message", message: 4, text: "x" });
    expect(JSON.parse(deleteMessageRequest(4))).toEqual({ type: "delete-message", message: 4 });
  });

  it("keeps the source for editing in the chat line", () => {
    const line = toChatMessage({ ...msg(7), text: "a <b>", edited: 9 });
    expect(line.source).toEqual({ id: 7, text: "a <b>" });
    expect(line.edited).toBe(9);
  });

  it("reads replies and keeps their quote", () => {
    const raw = { ...msg(8), reply: { id: 7, author: "bob", text: "q" } };
    expect(parseServerEvent(JSON.stringify({ type: "message", message: raw }))).toEqual({ type: "message", message: raw });
    expect(toChatMessage(raw).reply).toEqual({ id: 7, author: "bob", text: "q" });
  });

  it("shows a quote's picture only from our server's files", () => {
    const base = "https://chat.example";
    const raw = { ...msg(8), reply: { id: 7, author: "bob", text: "", image: "/files/k/cat.png?preview" } };
    expect(parseServerEvent(JSON.stringify({ type: "message", message: raw }))).toEqual({ type: "message", message: raw });
    expect(toChatMessage(raw, base).reply).toEqual({ id: 7, author: "bob", text: "", image: "https://chat.example/files/k/cat.png?preview" });
    // Elsewhere it would load by itself (the reader's IP to another host).
    const foreign = { ...raw, reply: { ...raw.reply, image: "https://evil.example/files/k/x.png" } };
    expect(toChatMessage(foreign, base).reply).toEqual({ id: 7, author: "bob", text: "" });
    const bad = { ...raw, reply: { ...raw.reply, image: 5 } };
    expect(parseServerEvent(JSON.stringify({ type: "message", message: bad }))).toBeNull();
  });
});

describe("read state from the server", () => {
  const raw = JSON.stringify({
    type: "ready",
    channels: [{ id: 1, name: "general" }, { id: 2, name: "random" }],
    voiceChats: true,
    reads: { "1": 4 },
    summary: [
      { channel: 1, latest: 9, mentions: 2 },
      { channel: 2, latest: 3, mentions: 0 },
      { channel: 7, voice: 5, latest: 6, mentions: 1 },
    ],
  });

  it("keeps read marks and the summary, and learns voice chats", () => {
    const s = reduceText(initialTextState, parseServerEvent(raw)!);
    expect(s.reads).toEqual({ 1: 4 });
    expect(s.summary).toEqual({ 1: { latest: 9, mentions: 2 }, 2: { latest: 3, mentions: 0 }, 7: { latest: 6, mentions: 1 } });
    expect(s.voiceChannels[5]).toBe(7);
  });

  it("marks channels unread by the summary, without loaded messages", () => {
    const s = reduceText(initialTextState, parseServerEvent(raw)!);
    expect([...unreadTextIds(s, { 1: 4, 2: 3 })]).toEqual([1]);
  });

  it("follows read marks of the account's other sessions; marks never go back", () => {
    const s = reduceText(initialTextState, parseServerEvent(raw)!);
    const ev = parseServerEvent(JSON.stringify({ type: "read", channel: 1, message: 9 }));
    expect(ev).toEqual({ type: "read", channel: 1, message: 9 });
    const read = reduceText(s, ev!);
    expect(read.reads).toEqual({ 1: 9 });
    expect([...unreadTextIds(read, read.reads)]).toEqual([2]);
    expect(mentionCounts(read, read.reads, "alice")).toEqual({ 7: 1 });
    expect(reduceText(read, { type: "read", channel: 1, message: 5 }).reads).toEqual({ 1: 9 });
    expect(parseServerEvent(JSON.stringify({ type: "read", channel: "1", message: 9 }))).toBeNull();
  });

  it("builds the mark-read request", () => {
    expect(JSON.parse(markReadRequest(1, 9))).toEqual({ type: "mark-read", channel: 1, message: 9 });
  });

  it("takes a mark that goes back (mark unread) as it is, and notes it", () => {
    const s = reduceText(initialTextState, { type: "read", channel: 1, message: 9 });
    const ev = parseServerEvent(JSON.stringify({ type: "read", channel: 1, message: 4, back: true }));
    expect(ev).toEqual({ type: "read", channel: 1, message: 4, back: true });
    const back = reduceText(s, ev!);
    expect(back.reads).toEqual({ 1: 4 });
    expect(back.backs).toEqual({ 1: { message: 4, n: 1 } });
    expect(reduceText(back, ev!).backs[1]!.n).toBe(2);
    expect(JSON.parse(markUnreadRequest(1, 5))).toEqual({ type: "mark-unread", channel: 1, message: 5 });
  });
});

describe("search", () => {
  it("keeps the latest results", () => {
    const ev = parseServerEvent(JSON.stringify({ type: "search-results", text: "pizza", messages: [msg(3)] }));
    expect(ev).toEqual({ type: "search-results", text: "pizza", messages: [msg(3)] });
    const s = reduceText(initialTextState, ev!);
    expect(s.search).toEqual({ text: "pizza", messages: [msg(3)] });
    expect(JSON.parse(searchRequest({ text: "pizza" }))).toEqual({ type: "search", text: "pizza" });
    expect(JSON.parse(searchRequest({ text: "", author: "bob", channel: 3 }))).toEqual({ type: "search", text: "", author: "bob", channel: 3 });
  });
});

describe("search results follow edits and deletes", () => {
  it("patches and drops them", () => {
    let s = reduceText(initialTextState, { type: "search-results", text: "x", messages: [msg(1), msg(2)] });
    s = reduceText(s, { type: "message-edited", message: { ...msg(1), text: "new" } });
    s = reduceText(s, { type: "messages-deleted", channel: 1, ids: [2] });
    expect(s.search?.messages.map((m) => [m.id, m.text])).toEqual([[1, "new"]]);
  });
});

describe("stickers", () => {
  const wave = { id: 7, name: "wave", url: "/api/stickers/7" };
  it("come in ready and with each change", () => {
    let s = reduceText(initialTextState, parseServerEvent(JSON.stringify({ type: "ready", channels: [], stickers: [wave] }))!);
    expect(s.stickers).toEqual([wave]);
    s = reduceText(s, parseServerEvent(JSON.stringify({ type: "stickers", stickers: [] }))!);
    expect(s.stickers).toEqual([]);
    expect(reduceText(initialTextState, parseServerEvent('{"type":"ready","channels":[]}')!).stickers).toEqual([]);
  });

  it("a sticker message keeps its sticker, also on display", () => {
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), text: "[sticker: wave]", sticker: { id: 7, name: "wave" } } }));
    expect(ev?.type === "message" && ev.message.sticker).toEqual({ id: 7, name: "wave" });
    expect(toChatMessage({ ...msg(1), text: "[sticker: wave]", sticker: { id: 7, name: "wave" } }).sticker).toEqual({ id: 7, name: "wave" });
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), sticker: { id: "x" } } }))).toBeNull();
  });

  it("are sent with no text", () => {
    expect(JSON.parse(sendRequest(3, "", undefined, 9, false, "n1", 7))).toEqual({ type: "send", channel: 3, text: "", reply: 9, nonce: "n1", sticker: 7 });
  });
});

describe("the own channel list", () => {
  const list = { hidden: { text: [2], voice: [] }, order: { text: [], voice: [5, 3] } };

  it("comes with ready (an account's), and its changes from the account's devices", () => {
    expect(initialTextState.channelList).toBeNull();
    const ready = parseServerEvent(JSON.stringify({ type: "ready", channels: [], channelList: list }));
    let s = reduceText(initialTextState, ready!);
    expect(s.channelList).toEqual(list);
    const next = { ...list, hidden: { text: [], voice: [7] } };
    s = reduceText(s, parseServerEvent(JSON.stringify({ type: "channel-list", list: next }))!);
    expect(s.channelList).toEqual(next);
    // Without an account the server sends none: the device keeps it.
    expect(reduceText(s, parseServerEvent(JSON.stringify({ type: "ready", channels: [] }))!).channelList).toBeNull();
  });
});

describe("attachments", () => {
  it("keep the preview's type (a GIF's video)", () => {
    const upload = { url: "/files/k/a.gif", name: "a.gif", type: "image/gif", size: 5, preview: "/files/k/a.gif?preview", previewType: "video/mp4" };
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { id: 1, channel: 1, author: "a", text: "", at: 1, uploads: [upload] } }));
    expect(ev?.type === "message" ? ev.message.uploads?.[0]?.previewType : null).toBe("video/mp4");
  });
});

describe("webhooks", () => {
  const hook = { id: 3, name: "CI", creator: "mod", created: 5 };
  it("keeps a channel's webhooks; the token of a new one is not state", () => {
    const list = parseServerEvent(JSON.stringify({ type: "webhooks", channel: 2, webhooks: [hook] }));
    expect(reduceText(initialTextState, list!).webhooks).toEqual({ 2: [hook] });
    expect(parseServerEvent(JSON.stringify({ type: "webhooks", channel: 2, webhooks: [{ ...hook, created: "x" }] }))).toBeNull();
    const made = parseServerEvent(JSON.stringify({ type: "webhook-created", channel: 2, id: 3, token: "t" }));
    expect(made).toEqual({ type: "webhook-created", channel: 2, id: 3, token: "t" });
    expect(reduceText(initialTextState, made!)).toBe(initialTextState);
  });
  it("asks for, makes, renames and deletes them", () => {
    expect(JSON.parse(manageRequest({ webhooks: 2 }))).toEqual({ type: "webhooks", channel: 2 });
    expect(JSON.parse(manageRequest({ createWebhook: 2, name: "CI" }))).toEqual({ type: "create-webhook", channel: 2, name: "CI" });
    expect(JSON.parse(manageRequest({ renameWebhook: 3, name: "Build" }))).toEqual({ type: "rename-webhook", webhook: 3, name: "Build" });
    expect(JSON.parse(manageRequest({ deleteWebhook: 3 }))).toEqual({ type: "delete-webhook", webhook: 3 });
    expect(JSON.parse(manageRequest({ removeWebhookPicture: 3 }))).toEqual({ type: "remove-webhook-picture", webhook: 3 });
    expect(JSON.parse(webhookPictureTokenRequest(3))).toEqual({ type: "webhook-picture-token", webhook: 3 });
  });
  it("keeps a changed picture's version (its posts load it anew)", () => {
    const ev = parseServerEvent(JSON.stringify({ type: "webhook-picture", webhook: 3, version: 9 }));
    expect(reduceText(initialTextState, ev!).webhookPictures).toEqual({ 3: 9 });
    expect(webhookPicture("https://s", 3, 9)).toBe("https://s/api/webhook-picture/3.png?v=9");
    expect(webhookPicture("https://s", 3)).toBe("https://s/api/webhook-picture/3.png");
  });
  it("marks a webhook's message", () => {
    const ev = parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), webhook: 3 } }));
    expect(ev?.type === "message" && ev.message.webhook).toBe(3);
    expect(toChatMessage({ ...msg(1), webhook: 3 }, "http://srv").webhook).toBe(3);
    expect(toChatMessage(msg(2), "http://srv")).not.toHaveProperty("webhook");
    expect(parseServerEvent(JSON.stringify({ type: "message", message: { ...msg(1), webhook: "3" } }))).toBeNull();
  });
});
