import type { Key } from "../i18n/en";
import { withoutSpoilers } from "./markdown";
import { t } from "./i18n.svelte";

// Threads (Kallisti server): a side conversation started from a message
// of a text channel, a chat of its own (its id is a chat id like a
// channel's). channel: the text channel; start: the message it started
// from; last: its newest message's time (or its creation); quote: the
// start message (author empty: deleted since; not on a message's own
// thread).
export type Thread = {
  id: number;
  channel: number;
  start: number;
  name: string;
  author: string;
  authorId?: number;
  at: number;
  last: number;
  replies: number;
  archived?: boolean;
  quote?: { id: number; author: string; text: string };
};

export type Threads = Readonly<Record<number, Thread>>;

const isNum = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
const isStr = (v: unknown): v is string => typeof v === "string";
const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object";

export const isThread = (v: unknown): v is Thread =>
  isObj(v) &&
  [v.id, v.channel, v.start, v.at, v.last, v.replies].every(isNum) &&
  isStr(v.name) &&
  isStr(v.author) &&
  (v.authorId === undefined || isNum(v.authorId)) &&
  (v.archived === undefined || typeof v.archived === "boolean") &&
  (v.quote === undefined || (isObj(v.quote) && isNum(v.quote.id) && isStr(v.quote.author) && isStr(v.quote.text)));

export const isThreads = (v: unknown): v is Thread[] => Array.isArray(v) && v.every(isThread);

// The threads we follow that we know (their unread messages and mentions
// count).
export const followedThreads = (follows: ReadonlySet<number>, threads: Threads) => [...follows].filter((id) => threads[id]);

// The threads we started in a channel (each took a slow mode turn there):
// by user id, or as a guest by name since we joined.
export const startedIn = (threads: Threads, channel: number, me: { name: string; userId: number | null } | undefined, since: number) =>
  Object.values(threads).filter(
    (t) =>
      t.channel === channel &&
      !!me &&
      (t.authorId !== undefined ? t.authorId === me.userId : me.userId === null && t.author.toLowerCase() === me.name.toLowerCase() && t.at >= since),
  );

// Threads by id, with these added (newer data wins).
export const withThreads = (threads: Threads, list: readonly Thread[]): Threads =>
  list.length ? { ...threads, ...Object.fromEntries(list.map((t) => [t.id, t])) } : threads;

const NAME_LENGTH = 40;

// A new thread's name: the start of its message as plain text (no
// spoilers or Markdown marks), cut at a word.
export const defaultThreadName = (text: string) => {
  const plain = withoutSpoilers(text).replace(/[*_~`>]+/g, "").replace(/\s+/g, " ").trim();
  if (plain.length <= NAME_LENGTH) return plain || t("chat.threadDefaultName");
  const cut = plain.slice(0, NAME_LENGTH);
  return `${cut.slice(0, cut.lastIndexOf(" ") > 0 ? cut.lastIndexOf(" ") : NAME_LENGTH)}…`;
};

// "5 min ago", for the last reply.
const ago = (at: number, now: number) => {
  const min = Math.floor((now - at) / 60_000);
  if (min < 1) return t("chat.agoJustNow");
  if (min < 60) return t("chat.agoMinutes", { count: min });
  if (min < 24 * 60) return t("chat.agoHours", { count: Math.floor(min / 60) });
  return t("chat.agoDays", { count: Math.floor(min / (24 * 60)) });
};

// The box under a start message.
export const replyBoxText = (thread: Thread, now: number) =>
  thread.replies === 0 ? t("chat.threadNoReplies") : t("chat.threadReplies", { count: thread.replies, ago: ago(thread.last, now) });

// A channel's active threads, the newest activity first (the sidebar).
export const activeThreads = (threads: Threads, channel: number): Thread[] =>
  Object.values(threads)
    .filter((t) => t.channel === channel && !t.archived)
    .sort((a, b) => b.last - a.last || b.id - a.id);

// The channel's thread list: the active ones, then the archived ones as
// loaded (the newest first), each once.
export const threadList = (threads: Threads, archived: readonly Thread[], channel: number): Thread[] => {
  const active = activeThreads(threads, channel);
  const seen = new Set(active.map((t) => t.id));
  return [...active, ...archived.filter((t) => !seen.has(t.id)).map((t) => threads[t.id] ?? t)];
};

// Where a link to a chat goes: a thread we know (of a channel we read), a
// text channel or direct chat, or a thread to ask the server for (a chat
// we do not know: maybe an archived thread); null: not here (voice
// chats).
export const chatTarget = <P>(
  chat: number,
  s: { threads: Threads; channels: readonly { id: number }[]; directs: Readonly<Record<number, P>>; voiceChannels: Readonly<Record<number, number>> },
): { thread: Thread } | { channel: number } | { direct: P } | { ask: number } | null => {
  const reads = (id: number) => s.channels.some((c) => c.id === id);
  const t = s.threads[chat];
  const peer = s.directs[chat];
  if (t) return reads(t.channel) ? { thread: t } : null;
  if (reads(chat)) return { channel: chat };
  if (peer) return { direct: peer };
  return Object.values(s.voiceChannels).includes(chat) ? null : { ask: chat };
};

// Requests about threads (the server checks the rights): one by id, start
// one from a message, rename, archive (or open again), delete, follow (or
// not), a channel's archived threads (a page before a thread), and how
// long threads stay active without messages (staff).
export type ThreadAction =
  | { get: number }
  | { create: number; name: string }
  | { rename: number; name: string }
  | { archive: number; on: boolean }
  | { remove: number }
  | { follow: number; on: boolean }
  | { archived: number; before?: number }
  | { archiveAfter: number };

const threadBody = (a: ThreadAction) => {
  if ("get" in a) return { type: "get-thread", channel: a.get };
  if ("create" in a) return { type: "create-thread", message: a.create, name: a.name };
  if ("rename" in a) return { type: "rename-thread", channel: a.rename, name: a.name };
  if ("archive" in a) return { type: "archive-thread", channel: a.archive, on: a.on };
  if ("remove" in a) return { type: "delete-thread", channel: a.remove };
  if ("follow" in a) return { type: "follow-thread", channel: a.follow, on: a.on };
  if ("archived" in a) return { type: "threads", channel: a.archived, ...(a.before ? { before: a.before } : {}) };
  return { type: "set-thread-archive", seconds: a.archiveAfter };
};

export const threadRequest = (a: ThreadAction) => JSON.stringify(threadBody(a));

// The choices for archiving idle threads (staff).
export const ARCHIVE_TIMES: readonly { seconds: number; label: Key }[] = [
  { seconds: 3600, label: "chat.archiveHour" },
  { seconds: 24 * 3600, label: "chat.archiveDay" },
  { seconds: 3 * 24 * 3600, label: "chat.archiveThreeDays" },
  { seconds: 7 * 24 * 3600, label: "chat.archiveWeek" },
];
