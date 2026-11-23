import { escapeHtml } from "@kallisti/protocol";
import type { TextState } from "./textChannels";
import { followedThreads } from "./threads";

// Mentions: "@" plus a user's whole name, any case, after a space or an
// opening mark (not in an address, a URL or right after an entity).
// Messages here are escaped HTML, so the name is matched escaped too; tags
// are skipped.
// What may not follow a whole name: a name's letter, or "@host"/".x" (then
// it is another server's account of the name, alice@host).
const NAME_CHAR = "[\\p{L}\\p{N}_-]|[@.][\\p{L}\\p{N}]";
const cached = new Map<string, RegExp>(); // a few names: mine, everyone, here
const pattern = (name: string) => {
  let re = cached.get(name);
  if (!re) {
    const escaped = escapeHtml(name).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    re = new RegExp(`(?<=^|[\\s(\\[{,:])@${escaped}(?!${NAME_CHAR})`, "giu");
    cached.set(name, re);
  }
  re.lastIndex = 0;
  return re;
};

// Text outside tags, then tags, alternating.
const parts = (html: string) => html.split(/(<[^>]*>)/);

export const mentions = (html: string, name: string): boolean =>
  name !== "" && parts(html).some((p, i) => i % 2 === 0 && pattern(name).test(p));

const sameName = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

// A message that mentions me, not written by me (author null: the server).
export const mentionsMe = (html: string, author: string | null, me: string): boolean =>
  (author === null || !sameName(author, me)) && mentions(html, me);

// "everyone" or "here": the server marked the message as a mention of all
// (only moderators can).
export const MENTION_ALL = ["everyone", "here"] as const;
export type MentionAll = (typeof MENTION_ALL)[number];
export const isMentionAll = (v: unknown): v is MentionAll => MENTION_ALL.includes(v as MentionAll);

// A message that pings me: it mentions me (or all), or answers a message
// of mine.
export const pingsMe = (html: string, author: string | null, replyTo: string | undefined, me: string, all?: MentionAll): boolean => {
  const mine = author !== null && sameName(author, me);
  return mentionsMe(html, author, me) || (!mine && (!!all || (replyTo !== undefined && sameName(replyTo, me))));
};

// Whose message a reply pings: the original's author, unless sent
// without a ping.
// A webhook's post may carry a user's name: its answers ping nobody.
export const replyPing = (m: { reply?: { author: string; webhook?: boolean }; noPing?: boolean }) =>
  m.noPing || m.reply?.webhook ? undefined : m.reply?.author;

// One pattern for many names (longest first, so "lain soy" wins over
// "lain"), cached for the last list.
let anyCache: { key: string; re: RegExp } | null = null;
const anyOf = (names: readonly string[]) => {
  const sorted = [...new Set(names.map((n) => n.toLowerCase()))].sort((a, b) => b.length - a.length);
  const key = sorted.join("\n");
  if (anyCache?.key !== key) {
    const alts = sorted.map((n) => escapeHtml(n).replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|");
    anyCache = { key, re: new RegExp(`(?<=^|[\\s(\\[{,:])@(?:${alts})(?!${NAME_CHAR})`, "giu") };
  }
  anyCache.re.lastIndex = 0;
  return anyCache.re;
};

// Mentions as pills: me (class "mention me"), all (when the server
// flagged it) and other known names; unknown names stay text.
export const highlightMentions = (html: string, name: string, all?: MentionAll, known: readonly string[] = []): string => {
  const names = [name, all ?? "", ...known].filter((n) => n !== "");
  if (names.length === 0) return html;
  const re = anyOf(names);
  return parts(html)
    .map((p, i) =>
      i % 2 === 0
        ? p.replace(re, (m) => `<span class="${name !== "" && sameName(unescape(m.slice(1)), name) ? "mention me" : "mention"}">${m}</span>`)
        : p,
    )
    .join("");
};
const unescape = (s: string) => s.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, "&");

// Unseen messages per channel that mention me (not my own), for badges:
// the server's count up to its summary (if not read that far), plus
// loaded messages after both. Messages are sorted by id.
// blocked: authors whose messages do not count (the server's summary from
// before the login still does).
export const mentionCounts = (
  state: TextState,
  seen: Record<number, number>,
  me: string,
  blocked: (name: string) => boolean = () => false,
): Record<number, number> => {
  const out: Record<number, number> = {};
  // Only chats that still exist: text channels, known voice chats and
  // direct chats (there every message from the other user counts).
  const ids = new Set([
    ...state.channels.map((c) => c.id),
    ...Object.values(state.voiceChannels),
    ...Object.keys(state.directs).map(Number),
    ...followedThreads(state.follows, state.threads),
  ]);
  for (const channel of ids) {
    const direct = channel in state.directs;
    const read = seen[channel] ?? 0;
    const summary = state.summary[channel];
    let n = summary && read < summary.latest ? summary.mentions : 0;
    const after = Math.max(read, summary?.latest ?? 0);
    const msgs = state.messages[channel] ?? [];
    for (let i = msgs.length - 1; i >= 0 && msgs[i]!.id > after; i--) {
      const m = msgs[i]!;
      if (blocked(m.author)) continue;
      if (direct ? m.author.toLowerCase() !== me.toLowerCase() : pingsMe(escapeHtml(m.text), m.author, replyPing(m), me, m.mentionAll)) n++;
    }
    if (n) out[channel] = n;
  }
  return out;
};

// Names "@" suggests: who is online, who wrote in the open chat, and the
// registered users who are offline (built-in server); for moderators also
// everyone and here (all).
export const mentionNames = (s: { online: readonly string[]; authors: readonly string[]; members: readonly string[]; all: boolean }): string[] => [
  ...new Set([...s.online, ...s.authors, ...s.members]),
  ...(s.all ? MENTION_ALL : []),
];

// The list of names before, when the new one has the same names: a
// derived list then stays the same object, and what reads it (each shown
// message) is not made again on every event.
export const keepNames = () => {
  let before: readonly string[] = [];
  return (now: readonly string[]) => (now.length === before.length && now.every((n, i) => n === before[i]) ? before : (before = now));
};

export const SUGGESTIONS = 8; // at most, in a suggestion list
const byName = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: "base" });

// Names to offer: those starting with the query first, then those that
// contain it; never me, each once.
export const suggestNames = (names: readonly string[], query: string, me: string): string[] => {
  const q = query.toLowerCase();
  const unique = [...new Map(names.map((n) => [n.toLowerCase(), n])).entries()].filter(([low]) => low !== me.toLowerCase());
  const starts = unique.filter(([low]) => low.startsWith(q)).map(([, n]) => n).sort(byName);
  const contains = unique.filter(([low]) => !low.startsWith(q) && low.includes(q)).map(([, n]) => n).sort(byName);
  return [...starts, ...contains].slice(0, SUGGESTIONS);
};
