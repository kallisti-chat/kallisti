import { t } from "../i18n.svelte";
import { fitSize } from "./protocol";

// What a streamer sends: a screen share and a camera (at most one each),
// at a quality the streamer picks, also while live.

export type Kind = "screen" | "camera";
export const KINDS: readonly Kind[] = ["screen", "camera"];
// height: of a landscape picture (the short side); fps: frames a second.
// bitrate: optional custom bitrate in bps.
export type Quality = { height: number; fps: number; bitrate?: number };
// The server's limits (its "ready"): the upload of both streams together,
// bytes a second, and the largest side of a picture.
export type Limits = { bytesPerSecond: number; maxSide: number; maxFps?: number };

const HEIGHTS = [480, 720, 1080];
const FPS = [15, 30, 60, 120];
export const DEFAULT_LIMITS: Limits = { bytesPerSecond: 1 << 20, maxSide: 4096, maxFps: 60 };
export const DEFAULT_QUALITY: Record<Kind, Quality> = { screen: { height: 720, fps: 30 }, camera: { height: 720, fps: 30 } };

// Of the upload: a part for sound and keyframes, the rest for one stream,
// or shared while screen and camera run together.
const HEADROOM = 0.8;
const SHARE: Record<Kind, number> = { screen: 0.65, camera: 0.35 };
const BITS_PER_PIXEL = 0.145; // per frame: 720p at 30 fps is 4 Mbit/s
const MIN_BITRATE = 100_000;

export const parseLimits = (v: unknown): Limits => {
  const l = (v ?? {}) as Record<string, unknown>;
  const pos = (n: unknown, d: number) => (typeof n === "number" && n > 0 ? n : d);
  return {
    bytesPerSecond: pos(l.bytesPerSecond, DEFAULT_LIMITS.bytesPerSecond),
    maxSide: pos(l.maxSide, DEFAULT_LIMITS.maxSide),
    maxFps: pos(l.maxFps, DEFAULT_LIMITS.maxFps ?? 60),
  };
};

// The encoder's settings for a source picture at a quality: the source's
// aspect within 16:9 of the chosen height (turned for a portrait picture)
// and the server's largest side; a bitrate for its size and frame rate,
// within the stream's part of the upload (together: the other kind runs
// too).
export const encoding = (source: { width: number; height: number }, q: Quality, kind: Kind, limits: Limits, together = false) => {
  const fps = limits.maxFps ? Math.min(q.fps, limits.maxFps) : q.fps;
  const [short, long] = [Math.min(q.height, limits.maxSide), Math.min(Math.ceil((q.height * 16) / 9), limits.maxSide)];
  const portrait = source.height > source.width;
  const size = fitSize(source.width, source.height, portrait ? short : long, portrait ? long : short);
  const budget = Math.floor(limits.bytesPerSecond * 8 * HEADROOM * (together ? SHARE[kind] : 1));
  const autoBitrate = Math.max(MIN_BITRATE, Math.min(budget, Math.round(size.width * size.height * fps * BITS_PER_PIXEL)));
  const bitrate = q.bitrate ? Math.max(MIN_BITRATE, Math.min(budget, q.bitrate)) : autoBitrate;
  return { ...size, framerate: fps, bitrate };
};

// A stored quality: one of the choices, else the default.
export const parseQuality = (v: unknown, kind: Kind): Quality => {
  const q = (v ?? {}) as Record<string, unknown>;
  const h = typeof q.height === "number" ? q.height : DEFAULT_QUALITY[kind].height;
  const f = typeof q.fps === "number" ? q.fps : DEFAULT_QUALITY[kind].fps;
  const br = typeof q.bitrate === "number" && q.bitrate > 0 ? q.bitrate : undefined;
  return {
    height: HEIGHTS.includes(h) ? h : DEFAULT_QUALITY[kind].height,
    fps: FPS.includes(f) ? f : DEFAULT_QUALITY[kind].fps,
    ...(br !== undefined && { bitrate: br }),
  };
};

export const qualityOptions = (limits: Limits) => ({
  heights: HEIGHTS.filter((h) => h <= limits.maxSide),
  fps: FPS.filter((f) => !limits.maxFps || f <= limits.maxFps),
});

export const qualityLabel = (q: Quality) => t("settings.qualityLabel", { height: q.height, fps: q.fps });
export const streamLabel = (name: string, camera: boolean) => t(camera ? "settings.streamCamera" : "settings.streamScreen", { name });

// What the browser can share (mobile browsers cannot share the screen).
export const sources = (env: { encode: boolean; display: boolean; camera: boolean; phone: boolean }): Record<Kind, boolean> => ({
  screen: env.encode && env.display && !env.phone,
  camera: env.encode && env.camera,
});

// The chosen camera (null: the default) at a quality.
export const cameraConstraints = (deviceId: string | null, q: Quality): MediaTrackConstraints => ({
  deviceId: deviceId ? { exact: deviceId } : undefined,
  width: { ideal: Math.ceil((q.height * 16) / 9) },
  height: { ideal: q.height },
  frameRate: { ideal: q.fps },
});

// The camera after the current one (a phone's front and back), or null.
export const nextCamera = (cameras: readonly { deviceId: string }[], current: string | null): string | null => {
  if (cameras.length < 2) return null;
  const i = cameras.findIndex((c) => c.deviceId === current);
  return cameras[(Math.max(0, i) + 1) % cameras.length]!.deviceId;
};
