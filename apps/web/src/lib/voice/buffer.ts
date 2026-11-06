import type { PlayoutConfig } from "./schedule";
import type { Key } from "../../i18n/en";
import { t } from "../i18n.svelte";

// How long playback waits at the start of a stream (see schedule.ts): more
// absorbs worse jitter, at the cost of delay. Music gets at least 500 ms:
// delay does not matter for one-way music, gaps do.
export type BufferPreset = "low" | "medium" | "high";
export type PlaybackBuffer = BufferPreset | number;
export const BUFFER_MS: Record<BufferPreset, number> = { low: 60, medium: 120, high: 250 };
export const BUFFERS: readonly BufferPreset[] = ["low", "medium", "high"];
const BUFFER_KEY: Record<BufferPreset, Key> = { low: "client.bufferLow", medium: "client.bufferMedium", high: "client.bufferHigh" };

export const MIN_BUFFER_MS = 30;
export const MAX_BUFFER_MS = 500;

export const toBufferMs = (b: PlaybackBuffer): number => {
  if (typeof b === "number") {
    return Math.max(MIN_BUFFER_MS, Math.min(MAX_BUFFER_MS, Math.round(b)));
  }
  return BUFFER_MS[b] ?? BUFFER_MS.low;
};

export const bufferLabel = (b: PlaybackBuffer) => {
  if (b === "low" || b === "medium" || b === "high") {
    return t(BUFFER_KEY[b], { ms: BUFFER_MS[b] });
  }
  if (b === BUFFER_MS.low) return t(BUFFER_KEY.low, { ms: BUFFER_MS.low });
  if (b === BUFFER_MS.medium) return t(BUFFER_KEY.medium, { ms: BUFFER_MS.medium });
  if (b === BUFFER_MS.high) return t(BUFFER_KEY.high, { ms: BUFFER_MS.high });
  return `${toBufferMs(b)} ms`;
};

const MUSIC_MS = 500;
// The queue may grow this much beyond the start delay before chunks are
// dropped (a burst after a stall, or clock drift).
const HEADROOM_MS = 240;
const SAMPLES_PER_MS = 48;

export const playoutFor = (buffer: PlaybackBuffer, music: boolean): PlayoutConfig => {
  const bufMs = toBufferMs(buffer);
  const ms = music ? Math.max(MUSIC_MS, bufMs) : bufMs;
  return { startDelay: ms * SAMPLES_PER_MS, maxLatency: (ms + HEADROOM_MS) * SAMPLES_PER_MS };
};
