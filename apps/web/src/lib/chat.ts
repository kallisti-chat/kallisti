import type { ChatMessage } from "@kallisti/protocol";

export type MessageGroup = {
  actor: number | null;
  actorName: string | null;
  at: number;
  messages: ChatMessage[];
};

const GROUP_WINDOW_MS = 5 * 60_000;

const sameDay = (a: number, b: number) => new Date(a).toDateString() === new Date(b).toDateString();

// Discord-style grouping: one header for a run of messages from one sender,
// within 5 minutes and on the same day.
const webhookOf = (m: ChatMessage | undefined) => (m && "webhook" in m ? m.webhook : undefined);

// newFrom: the first unread message (a group starts there, see chatItems).
const idOf = (m: ChatMessage) => ("source" in m ? (m.source as { id: number } | undefined)?.id : undefined);

export const groupMessages = (messages: readonly ChatMessage[], newFrom: number | null = null): MessageGroup[] => {
  const groups: MessageGroup[] = [];
  for (const m of messages) {
    const last = groups.at(-1);
    const prev = last?.messages.at(-1);
    // A webhook's post may carry a user's name (see lib/webhooks).
    const sameAuthor = last?.actor === m.actor && last.actorName === m.actorName && webhookOf(prev) === webhookOf(m);
    const fresh = newFrom !== null && idOf(m) === newFrom;
    if (last && prev && sameAuthor && !fresh && m.at - prev.at <= GROUP_WINDOW_MS && sameDay(prev.at, m.at)) {
      last.messages.push(m);
    } else {
      groups.push({ actor: m.actor, actorName: m.actorName, at: m.at, messages: [m] });
    }
  }
  return groups;
};

export type ChatItem =
  | { kind: "day"; at: number }
  | { kind: "new" }
  | { kind: "group"; group: MessageGroup }
  | { kind: "blocked"; groups: MessageGroup[] };

// Groups with a date divider before the first group of each day. Groups
// of blocked users in a row (on one day) are one "blocked" item. newFrom:
// the "NEW" line before this message (see lib/unread).
export const chatItems = (messages: readonly ChatMessage[], blocked: (name: string) => boolean = () => false, newFrom: number | null = null): ChatItem[] => {
  const items: ChatItem[] = [];
  let prev: MessageGroup | undefined;
  for (const group of groupMessages(messages, newFrom)) {
    const hidden = group.actorName !== null && blocked(group.actorName);
    if (!prev || !sameDay(prev.at, group.at)) items.push({ kind: "day", at: group.at });
    if (newFrom !== null && idOf(group.messages[0]!) === newFrom) items.push({ kind: "new" });
    prev = group;
    const last = items.at(-1);
    if (hidden && last?.kind === "blocked") last.groups.push(group);
    else items.push(hidden ? { kind: "blocked", groups: [group] } : { kind: "group", group });
  }
  return items;
};

// Keys of the message rows: stored messages by id (a reaction or an edit
// makes a new object), Mumble chat lines by their object (it never
// changes), as a number given on first sight.
const lineKeys = new WeakMap<ChatMessage, string>();
let nextLine = 0;
export const messageKey = (m: ChatMessage & { source?: { id: number } }) => {
  if (m.source) return `s${m.source.id}`;
  let key = lineKeys.get(m);
  if (!key) lineKeys.set(m, (key = `m${nextLine++}`));
  return key;
};

// Groups (and hidden blocked ones) by their first message.
export const itemKey = (item: ChatItem) =>
  item.kind === "day" ? `day-${item.at}` : item.kind === "new" ? "new" : messageKey((item.kind === "group" ? item.group : item.groups[0]!).messages[0]!);

// "auto": as the language has it (12-hour for English).
export type TimeFormat = "auto" | "12" | "24";

export const formatTime = (at: number, locale?: string, clock: TimeFormat = "auto") =>
  new Date(at).toLocaleTimeString(locale, {
    hour: clock === "24" ? "2-digit" : "numeric",
    minute: "2-digit",
    ...(clock !== "auto" && { hourCycle: clock === "24" ? "h23" : "h12" }),
  });

export const formatDay = (at: number, locale?: string) =>
  new Date(at).toLocaleDateString(locale, { year: "numeric", month: "long", day: "numeric" });

// "Today at 3:58 PM", "Yesterday at 9:05 AM", else "10/1/2026 9:05 AM".
export const formatTimestamp = (at: number, now: number, locale?: string, clock: TimeFormat = "auto") => {
  const time = formatTime(at, locale, clock);
  if (sameDay(at, now)) return `Today at ${time}`;
  if (sameDay(at, now - 86_400_000)) return `Yesterday at ${time}`;
  return `${new Date(at).toLocaleDateString(locale)} ${time}`;
};

// Unread private messages per sender name: received minus read.
export const unreadCounts = (
  counts: Readonly<Record<string, number>>,
  read: Readonly<Record<string, number>>,
): Record<string, number> =>
  Object.fromEntries(
    Object.entries(counts)
      .map(([name, n]) => [name, n - (read[name] ?? 0)] as const)
      .filter(([, n]) => n > 0),
  );
