import type { Key } from "../i18n/en";
import { i18n } from "./i18n.svelte";

// Per text channel, per server (channel ids are the server's): which
// messages notify, and a mute (until a time; 0: until turned off). Muted
// channels show no unread marker and notify only for mentions, as in
// Discord; "nothing" never notifies (badges still show). Threads take
// their channel's setting until they get their own (see withParents).
export type NotifyLevel = "all" | "mentions" | "nothing";
export type ChannelPref = { level?: NotifyLevel; mutedUntil?: number };
export type ChannelPrefs = Readonly<Record<string, Readonly<Record<number, ChannelPref>>>>;

// Labels are text keys (t() where they show).
export const LEVELS: readonly [NotifyLevel, Key][] = [
  ["all", "settings.notifyAll"],
  ["mentions", "settings.notifyMentions"],
  ["nothing", "settings.notifyNothing"],
];
const DEFAULT_LEVEL: NotifyLevel = "mentions";
export const levelOf = (p: ChannelPref): NotifyLevel => p.level ?? DEFAULT_LEVEL;
const isLevel = (v: unknown): v is NotifyLevel => LEVELS.some(([l]) => l === v);

const parsePref = (v: unknown): ChannelPref | null => {
  if (!v || typeof v !== "object") return null;
  const { level, mutedUntil } = v as Record<string, unknown>;
  const pref: ChannelPref = {
    ...(isLevel(level) && { level }),
    ...(typeof mutedUntil === "number" && mutedUntil >= 0 && { mutedUntil }),
  };
  return Object.keys(pref).length ? pref : null;
};

// Also the old form: a list of muted channel ids per server.
export const parseChannelPrefs = (raw: string | null): ChannelPrefs => {
  let data: unknown;
  try {
    data = JSON.parse(raw ?? "null");
  } catch {
    return {};
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) return {};
  const out: Record<string, Record<number, ChannelPref>> = {};
  for (const [server, v] of Object.entries(data)) {
    if (!v || typeof v !== "object") continue;
    const entries: [unknown, unknown][] = Array.isArray(v)
      ? v.filter((id) => Number.isInteger(id)).map((id) => [id, { mutedUntil: 0 }])
      : Object.entries(v);
    out[server] = Object.fromEntries(
      entries.flatMap(([id, p]) => {
        const pref = parsePref(p);
        return Number.isInteger(Number(id)) && pref ? [[Number(id), pref]] : [];
      }),
    );
  }
  return out;
};

export const prefOf = (prefs: ChannelPrefs, server: string, id: number): ChannelPref => prefs[server]?.[id] ?? {};

const patched = (p: ChannelPref, patch: ChannelPref) =>
  Object.fromEntries(Object.entries({ ...p, ...patch }).filter(([, v]) => v !== undefined)) as ChannelPref;

// next: {} forgets the setting.
const withPref = (prefs: ChannelPrefs, server: string, id: number, next: ChannelPref): ChannelPrefs => {
  const { [id]: _, ...rest } = prefs[server] ?? {};
  return { ...prefs, [server]: Object.keys(next).length ? { ...rest, [id]: next } : rest };
};

// patch: undefined removes a field; the default level is not stored.
export const setChannelPref = (prefs: ChannelPrefs, server: string, id: number, patch: ChannelPref): ChannelPrefs => {
  const { level, ...next } = patched(prefOf(prefs, server, id), patch);
  return withPref(prefs, server, id, level && level !== DEFAULT_LEVEL ? { level, ...next } : next);
};

// A thread's own setting: the patch on the one it has now (maybe its
// channel's), kept whole with its level, so that the default level and
// no mute hold against the channel's too.
export const setThreadPref = (prefs: ChannelPrefs, server: string, id: number, now: ChannelPref, patch: ChannelPref): ChannelPrefs => {
  const next = patched(now, patch);
  return withPref(prefs, server, id, { ...next, level: levelOf(next) });
};

export const forgetPref = (prefs: ChannelPrefs, server: string, id: number) => withPref(prefs, server, id, {});

// The settings as they hold: threads (by id, with their channel) without
// their own take their channel's.
export const withParents = (prefs: ChannelPrefs, server: string, threads: Readonly<Record<number, { channel: number }>>): ChannelPrefs => {
  const own = prefs[server] ?? {};
  const taken = Object.entries(threads).flatMap(([id, { channel }]) => (own[Number(id)] || !own[channel] ? [] : [[id, own[channel]!] as const]));
  return taken.length ? { ...prefs, [server]: { ...Object.fromEntries(taken), ...own } } : prefs;
};

// The mute choices (text key, minutes; 0: until turned off) and when one ends.
export const MUTE_FOR: readonly [Key, number][] = [
  ["settings.muteFor15Minutes", 15],
  ["settings.muteFor1Hour", 60],
  ["settings.muteFor8Hours", 8 * 60],
  ["settings.muteFor24Hours", 24 * 60],
  ["settings.muteUntilOn", 0],
];
export const muteUntil = (minutes: number, now: number) => (minutes ? now + minutes * 60_000 : 0);
// null: no end.
export const mutedUntilText = (t: number | undefined) =>
  t ? new Date(t).toLocaleString(i18n.locale, { weekday: "short", hour: "numeric", minute: "2-digit" }) : null;

export const isMuted = (p: ChannelPref, now: number) => p.mutedUntil !== undefined && (p.mutedUntil === 0 || p.mutedUntil > now);

export const mutedIds = (prefs: ChannelPrefs, server: string, now: number): Set<number> =>
  new Set(
    Object.entries(prefs[server] ?? {})
      .filter(([, p]) => isMuted(p, now))
      .map(([id]) => Number(id)),
  );

// Channels set to "nothing": the server sends no push notifications for
// them to this browser (mutes still let mentions through).
export const nothingIds = (prefs: ChannelPrefs, server: string): number[] =>
  Object.entries(prefs[server] ?? {})
    .filter(([, p]) => p.level === "nothing")
    .map(([id]) => Number(id));

// What the server learns for push notifications (see lib/push): quiet:
// chats set to "nothing" and direct chats muted for good (a timed mute
// would outlive closed tabs); loud: chats with another level of their
// own (threads with a setting of their own have one: the server keeps
// the threads of quiet channels quiet, not these).
export const pushLists = (prefs: ChannelPrefs, server: string, directs: readonly number[]) => {
  const own = prefs[server] ?? {};
  return {
    quiet: [...nothingIds(prefs, server), ...directs.filter((id) => own[id]?.mutedUntil === 0)].sort((a, b) => a - b),
    loud: Object.entries(own)
      .filter(([, p]) => p.level && p.level !== "nothing")
      .map(([id]) => Number(id)),
  };
};

export const withoutMuted = (unread: ReadonlySet<number>, muted: ReadonlySet<number>): Set<number> =>
  new Set([...unread].filter((id) => !muted.has(id)));

// Does a message notify (desktop notification, sound)? mentioned: it
// pings me.
export const notifies = (p: ChannelPref, mentioned: boolean, now: number): boolean => {
  if (p.level === "nothing") return false;
  if (mentioned) return true;
  return p.level === "all" && !isMuted(p, now);
};
