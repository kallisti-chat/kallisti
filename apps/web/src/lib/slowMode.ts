import { t } from "./i18n.svelte";

// Slow mode (built-in server): in a text channel, each user may post once
// per so many seconds; staff are exempt. The server checks and refuses
// with when the wait ends (see TextState.slowUntil).

// The choices in seconds (0: off), as the server has them.
export const SLOW_MODES = [0, 5, 30, 60, 300, 3600] as const;

// "1 h 1 min", "1 min 5 s", "23 s": empty parts left out.
export const formatSeconds = (seconds: number) => {
  const parts = [
    [Math.floor(seconds / 3600), "admin.unitHours"],
    [Math.floor((seconds % 3600) / 60), "admin.unitMinutes"],
    [seconds % 60, "admin.unitSeconds"],
  ] as const;
  return parts.flatMap(([count, unit]) => (count ? [t(unit, { count })] : [])).join(" ") || t("admin.unitSeconds", { count: 0 });
};

export const slowModeLabel = (seconds: number) => (seconds ? formatSeconds(seconds) : t("common.off"));

// How long (ms) we still wait before we may post: from our last post here
// or the server's refusal (until), whichever ends later. Both are server
// times: at most the slow mode itself, whatever our clock says.
export const slowLeft = (seconds: number, lastPost: number | undefined, until: number | undefined, now: number) => {
  if (seconds <= 0) return 0;
  const ends = Math.max(lastPost === undefined ? 0 : lastPost + seconds * 1000, until ?? 0);
  return Math.min(Math.max(ends - now, 0), seconds * 1000);
};

// When we last posted among the messages (undefined: not among them).
export const lastPostAt = <M extends { at: number }>(messages: readonly M[], mine: (m: M) => boolean) =>
  messages.reduce<number | undefined>((last, m) => (mine(m) && (last === undefined || m.at > last) ? m.at : last), undefined);

// Our last turn in a text channel: a post there, or a thread we started
// from one of its messages (the server counts both).
export const lastTurnAt = <M extends { at: number }>(messages: readonly M[], mine: (m: M) => boolean, starts: readonly { at: number }[]) =>
  starts.reduce((last, t) => (last === undefined || t.at > last ? t.at : last), lastPostAt(messages, mine));
