import { toBufferMs, type PlaybackBuffer } from "./voice/buffer";
import { t } from "./i18n.svelte";

// The connection info next to "Voice connected": ping and late or lost
// audio over the last 30 s, and a level from them.

export const WINDOW_MS = 30_000;

export type Level = "good" | "fair" | "poor";
// Audio chunks from all speakers since the connection started (see
// voice/schedule chunkOutcome).
// musicLate: the late ones of music speakers (part of late).
export type Counters = { good: number; late: number; lost: number; musicLate?: number };

export const recent = <T extends { at: number }>(items: readonly T[], now: number): T[] =>
  items.filter((i) => now - i.at <= WINDOW_MS);

// The variance, not the spread: Mumble's Ping wants it (clients show the
// square root).
export const meanVariance = (values: readonly number[]): { mean: number; variance: number } | null => {
  if (!values.length) return null;
  const mean = values.reduce((a, b) => a + b, 0) / values.length;
  return { mean, variance: values.reduce((a, v) => a + (v - mean) ** 2, 0) / values.length };
};

// Late and lost audio in percent of all audio between two snapshots (null:
// no audio came).
const percentOf = (from: Counters, to: Counters, bad: number): number | null => {
  const total = to.good - from.good + (to.late - from.late) + (to.lost - from.lost);
  return total > 0 ? (bad / total) * 100 : null;
};
export const badPercent = (from: Counters, to: Counters) => percentOf(from, to, to.late - from.late + (to.lost - from.lost));
// Late only: what a larger playback buffer helps against.
export const latePercent = (from: Counters, to: Counters) => percentOf(from, to, to.late - from.late);
// Late audio of speech only: what the playback buffer setting changes.
export const speechLatePercent = (from: Counters, to: Counters) =>
  percentOf(from, to, to.late - from.late - ((to.musicLate ?? 0) - (from.musicLate ?? 0)));

// Discord-like: at 100 ms people start to talk over each other, beyond
// 250 ms talking is hard. Late audio is a gap in the speech: a few in 100
// can pass, one in 12 is choppy. The worse of both counts.
const LIMITS: [Level, number, number][] = [
  ["good", 100, 2],
  ["fair", 250, 8],
];

// rtt: the mean ping in ms (null: none came back yet). bad: see badPercent.
export const qualityLevel = (rtt: number | null, bad: number | null): Level | null =>
  rtt === null ? null : (LIMITS.find(([, ms, pct]) => rtt < ms && (bad ?? 0) < pct)?.[0] ?? "poor");

// Late audio from "fair" on (see LIMITS): suggest the next playback
// buffer, if there is one.
export const biggerBuffer = (late: number | null, buffer: PlaybackBuffer): PlaybackBuffer | null => {
  if (late === null || late < LIMITS[0]![2]) return null;
  if (buffer === "low") return "medium";
  if (buffer === "medium") return "high";
  if (typeof buffer === "number") {
    if (buffer < 120) return "medium";
    if (buffer < 250) return "high";
    if (buffer < 500) return Math.min(500, buffer + 50);
  }
  return null;
};

export const connectionLabel = (level: Level | null, rtt: number | null) =>
  level === null || rtt === null
    ? t("client.connectionMeasuring")
    : t(level === "good" ? "client.connectionGood" : level === "fair" ? "client.connectionFair" : "client.connectionPoor", { ms: Math.round(rtt) });

// The desktop app's bridge (see lib/desktop bridgeUrl) sends voice over UDP
// while UDP works; the page cannot see which.
export const transportOf = (url: string) =>
  url.startsWith("ws://127.0.0.1:") && url.includes("secret=") ? t("client.transportDesktop") : "WebSocket";
