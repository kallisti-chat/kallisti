import type { Key } from "../i18n/en";
import { t } from "./i18n.svelte";

// Online, idle, do not disturb, and a custom status (emoji and text, until
// a time). The Kallisti server keeps them; Mumble clients do not see them.

export type Presence = "online" | "idle" | "dnd";
// until: Unix milliseconds; after it, the custom status is gone.
export type PresenceView = { status: Presence; emoji?: string; text?: string; until?: number };
export type CustomStatus = { emoji?: string; text?: string; until?: number };

const PRESENCE_KEY: Record<Presence, Key> = { online: "client.presenceOnline", idle: "client.presenceIdle", dnd: "client.presenceDnd" };
// In the language in use when read (getters: reactive in templates).
export const PRESENCE_LABEL: Readonly<Record<Presence, string>> = {
  get online() {
    return t(PRESENCE_KEY.online);
  },
  get idle() {
    return t(PRESENCE_KEY.idle);
  },
  get dnd() {
    return t(PRESENCE_KEY.dnd);
  },
};
export const MAX_STATUS_TEXT = 128; // characters, as the server

// No input (or a hidden page) this long: away (shown as idle).
export const AWAY_AFTER_MS = 10 * 60_000;

// What others see: dnd if chosen, idle if chosen or away, else online.
export const effectivePresence = (chosen: Presence, away: boolean): Presence =>
  chosen === "dnd" ? "dnd" : chosen === "idle" || away ? "idle" : "online";

export const isAway = (s: { lastInput: number; hiddenSince: number | null; now: number }) =>
  s.now - s.lastInput >= AWAY_AFTER_MS || (s.hiddenSince !== null && s.now - s.hiddenSince >= AWAY_AFTER_MS);

// The custom status to show (null: none, or its end has passed).
export const customOf = (v: PresenceView | undefined, now: number): { emoji: string; text: string } | null =>
  v && (v.emoji || v.text) && !(v.until && v.until <= now) ? { emoji: v.emoji ?? "", text: v.text ?? "" } : null;

// What the member list and menus show of a user.
export type UserStatus = { presence: Presence; custom: { emoji: string; text: string } | null };

// A session's status (web clients that are not plainly online are listed).
export const presenceOf = (statuses: Readonly<Record<number, PresenceView>>, session: number): Presence =>
  statuses[session]?.status ?? "online";

// A user's status. Our own: as we chose it, with away at once (the server's
// echo comes later).
export const statusOfSession = (
  s: { statuses: Readonly<Record<number, PresenceView>>; own: PresenceView; me: number | null; away: boolean; now: number },
  session: number,
): UserStatus =>
  session === s.me
    ? { presence: effectivePresence(s.own.status, s.away), custom: customOf(s.own, s.now) }
    : { presence: presenceOf(s.statuses, session), custom: customOf(s.statuses[session], s.now) };

export type ClearAfter = "30m" | "1h" | "4h" | "today" | "never";
export const CLEAR_AFTER: { value: ClearAfter; label: Key }[] = [
  { value: "30m", label: "client.clearAfter30m" },
  { value: "1h", label: "client.clearAfter1h" },
  { value: "4h", label: "client.clearAfter4h" },
  { value: "today", label: "client.clearAfterToday" },
  { value: "never", label: "client.clearAfterNever" },
];

const MINUTE = 60_000;
const AFTER: Record<Exclude<ClearAfter, "today" | "never">, number> = { "30m": 30 * MINUTE, "1h": 60 * MINUTE, "4h": 240 * MINUTE };

// The end of a custom status ("today": the next local midnight).
export const clearAfterUntil = (choice: ClearAfter, now: number): number | undefined => {
  if (choice === "never") return undefined;
  if (choice !== "today") return now + AFTER[choice];
  const d = new Date(now);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1).getTime();
};

const isPresence = (v: unknown): v is Presence => v === "online" || v === "idle" || v === "dnd";

// Network input: empty fields are left out.
export const parsePresence = (v: unknown): PresenceView | null => {
  if (!v || typeof v !== "object") return null;
  const { status, emoji, text, until } = v as Record<string, unknown>;
  if (!isPresence(status)) return null;
  return {
    status,
    ...(typeof emoji === "string" && emoji && { emoji }),
    ...(typeof text === "string" && text && { text }),
    ...(typeof until === "number" && until > 0 && { until }),
  };
};

export const setStatusRequest = (status: Presence, custom: CustomStatus = {}) =>
  JSON.stringify({ type: "set-status", status, ...custom });

export const awayRequest = (on: boolean) => JSON.stringify({ type: "away", on });
