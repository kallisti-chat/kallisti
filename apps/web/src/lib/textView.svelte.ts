import { untrack } from "svelte";
import type { Connection } from "./connection.svelte";
import { mentionCounts } from "./mentions";
import { autoSeen, pageAnswered, unreadTextIds } from "./textChannels";
import { nextLines } from "./unread";

// Text channel behaviour of the client view: load history once per channel,
// page back, track what was seen. Call during component setup (it uses
// effects). `currentIds` are the open chats (the main one and an open
// thread; null: none).
// `me` is our name, for mentions. `visible`: the tab is visible.
const MARK_DELAY_MS = 2000;
const PAGE_WAIT_MS = 10_000;

// blocked: users whose messages do not count as mentions.
export const createTextView = (
  connection: () => Connection,
  currentIds: () => readonly (number | null)[],
  me: () => string,
  blocked: (name: string) => boolean,
  visible: () => boolean,
) => {
  const text = $derived(connection().text);
  const requested = new Set<number>();
  const open = () => currentIds().filter((id) => id !== null);
  let seen = $state<Record<number, number>>({});
  // The pages of older messages asked for (by channel: the oldest id
  // then), until their answers, an error, or PAGE_WAIT_MS (a lost answer,
  // an error like the last one: scrolling asks again).
  let pages = $state<Record<number, number>>({});
  const endPage = (channel: number, before: number) => {
    if (pages[channel] !== before) return;
    const { [channel]: _, ...rest } = pages;
    pages = rest;
  };

  // In one effect with the load, so the order is fixed: after a resync the
  // history is gone and the open channel loads again.
  let seenResyncs = 0;
  $effect(() => {
    if (text.resyncs !== seenResyncs) {
      seenResyncs = text.resyncs;
      requested.clear();
    }
    for (const id of open()) {
      if (text.loaded[id] || requested.has(id)) continue;
      requested.add(id);
      connection().requestHistory(id);
    }
  });

  // An error answers some request: allow retries, end a pending page load.
  // (Derived: text is new on every event, the error only on a new one.)
  const lastError = $derived(text.lastError);
  $effect(() => {
    if (!lastError) return;
    untrack(() => {
      requested.clear();
      pages = {};
    });
  });

  $effect(() => {
    for (const [channel, before] of Object.entries(pages)) if (pageAnswered(text, { channel: Number(channel), before })) endPage(Number(channel), before);
  });

  // The "New" line by open chat (see lib/unread), and chats marked unread
  // here: while one stays open, nothing in it counts as seen (it is held
  // at that mark until it closes, or Mark as read). watched: the chats in
  // view at the last check (loaded, the tab visible).
  let lines = $state<Record<number, number>>({});
  let held = $state<Record<number, number>>({});
  let watched = new Set<number>();
  const without = (r: Record<number, number>, id: number) => {
    const { [id]: _, ...rest } = r;
    return rest;
  };

  // New messages in the open chats count as seen, while the tab is visible
  // (and when it becomes visible again). Before that, a chat that comes
  // into view gets its line where it was read.
  $effect(() => {
    const [state, ids, shown] = [text, open(), visible()];
    untrack(() => {
      const fresh = autoSeen(state, ids.filter((id) => !(id in held)), read, shown);
      const next = nextLines(lines, { open: ids, watched, seen: fresh, read });
      if (JSON.stringify(next) !== JSON.stringify(lines)) lines = next;
      watched = new Set(shown ? ids.filter((id) => state.loaded[id]) : []);
      for (const [id, newest] of fresh) {
        seen = { ...seen, [id]: newest };
        queueMark(id, newest);
      }
    });
  });
  // A held chat is free again once closed.
  $effect(() => {
    const ids = open();
    untrack(() => {
      for (const id of Object.keys(held).map(Number)) if (!ids.includes(id)) held = without(held, id);
    });
  });
  // A mark that went back (here or in another session): our own seen mark
  // follows, else it would send the old one again.
  const backsSeen = new Map<number, number>();
  $effect(() => {
    const backs = text.backs;
    untrack(() => {
      for (const [key, b] of Object.entries(backs)) {
        const id = Number(key);
        if (backsSeen.get(id) === b.n) continue;
        backsSeen.set(id, b.n);
        pending.delete(id);
        if ((seen[id] ?? 0) > b.message) seen = { ...seen, [id]: b.message };
      }
    });
  });

  // Read marks go to the server (registered users keep them), at most every
  // few seconds. After a reconnect, marks the server missed go again.
  const pending = new Map<number, number>();
  let flush: ReturnType<typeof setTimeout> | undefined;
  const queueMark = (id: number, message: number) => {
    pending.set(id, Math.max(pending.get(id) ?? 0, message));
    flush ??= setTimeout(() => {
      flush = undefined;
      for (const [channel, m] of pending) connection().markRead(channel, m);
      pending.clear();
    }, MARK_DELAY_MS);
  };
  $effect(() => {
    const reads = text.reads;
    untrack(() => {
      for (const [id, n] of Object.entries(seen)) if (n > (reads[Number(id)] ?? 0)) queueMark(Number(id), n);
    });
  });

  // What was read: here, or before (the server's read marks); a held chat
  // at its mark.
  const read = $derived.by(() => {
    const out: Record<number, number> = { ...text.reads };
    for (const [id, n] of Object.entries(seen)) out[Number(id)] = Math.max(out[Number(id)] ?? 0, n);
    for (const [id, n] of Object.entries(held)) out[Number(id)] = n;
    return out;
  });
  const unread = $derived(unreadTextIds(text, read));
  // Unseen mentions of us per channel (text channels and voice chats).
  const mentions = $derived(mentionCounts(text, read, me(), blocked));

  return {
    get unread() {
      return unread;
    },
    get mentions() {
      return mentions;
    },
    loadingOlder: (id: number | null) => id !== null && id in pages,
    // Read up to the newest message, as opening the chat would; its line
    // goes.
    markRead: (id: number) => {
      held = without(held, id);
      lines = without(lines, id);
      const newest = Math.max(text.messages[id]?.at(-1)?.id ?? 0, text.summary[id]?.latest ?? 0);
      if ((read[id] ?? 0) >= newest) return;
      seen = { ...seen, [id]: newest };
      queueMark(id, newest);
    },
    // Unread from a message on: the line moves there, and the chat stays
    // unread while it is open (accounts: the server keeps it).
    markUnread: (id: number, message: number) => {
      pending.delete(id);
      seen = { ...seen, [id]: message - 1 };
      held = { ...held, [id]: message - 1 };
      lines = { ...lines, [id]: message - 1 };
      connection().markUnread(id, message);
    },
    // We sent a message there: the line goes (as in Discord).
    clearLine: (id: number) => {
      if (id in lines) lines = without(lines, id);
    },
    // The mark the chat's "New" line follows (null: none).
    lineAfter: (id: number | null) => (id === null ? null : (lines[id] ?? null)),
    loadOlder: (id: number | null) => {
      const oldest = id === null ? undefined : text.messages[id]?.[0];
      if (id === null || !oldest || id in pages) return;
      pages = { ...pages, [id]: oldest.id };
      setTimeout(() => endPage(id, oldest.id), PAGE_WAIT_MS);
      connection().requestHistory(id, oldest.id);
    },
  };
};
