// The "NEW" line above the first unread message of a chat, and the bar
// that leads back to it (see ChatPane, textView).

type Line = { source?: { id: number }; actorName: string | null; at: number };

const idOf = (m: Line) => m.source?.id ?? 0;

// The first message after the read mark that someone else wrote (null:
// none, or no mark).
export const firstUnread = <M extends Line>(messages: readonly M[], after: number | null, mine: (m: M) => boolean) =>
  after === null ? null : (messages.find((m) => idOf(m) > after && !mine(m))?.source?.id ?? null);

// The bar: how many unread messages of others are loaded, since when; "N+"
// while the first unread one is not loaded yet (first: null).
export const unreadBar = <M extends Line>(messages: readonly M[], after: number | null, mine: (m: M) => boolean, time: (at: number) => string) => {
  if (after === null) return null;
  const unread = messages.filter((m) => idOf(m) > after && !mine(m));
  if (!unread.length) return null;
  const n = unread.length;
  const loaded = messages.some((m) => idOf(m) > 0 && idOf(m) <= after);
  const count = `${n}${loaded ? "" : "+"} new message${n === 1 && loaded ? "" : "s"}`;
  return loaded ? { first: idOf(unread[0]!), text: `${count} since ${time(unread[0]!.at)}` } : { first: null, text: count };
};

// The lines by chat: set where the chat was read when it comes into view
// with unread messages (seen: what the view now counts as seen, newly;
// watched: in view before), kept while it stays open, dropped once
// closed. None without an earlier read mark (a first visit).
export const nextLines = (
  lines: Readonly<Record<number, number>>,
  { open, watched, seen, read }: { open: readonly number[]; watched: ReadonlySet<number>; seen: readonly [number, number][]; read: Readonly<Record<number, number>> },
): Record<number, number> => {
  const out: Record<number, number> = {};
  for (const id of open) if (id in lines) out[id] = lines[id]!;
  for (const [id] of seen) if (!(id in out) && !watched.has(id) && (read[id] ?? 0) > 0) out[id] = read[id]!;
  return out;
};
