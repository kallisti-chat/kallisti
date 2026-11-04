import { may, Perm, selfOf, type ServerState } from "@kallisti/protocol";
import { t } from "./i18n.svelte";
import { formatDuration } from "./media";
import { roleOf } from "./roles";
import type { TextState } from "./textChannels";

// Channel recordings (built-in server): admins and moderators record a
// voice channel; the server writes one Ogg Opus file per speaker. Finished
// ones wait for download (a zip) for some days. Times: Unix milliseconds.
export type RecordingTrack = { name: string; file: string; channels: number; start: number };
export type FinishedRecording = {
  id: string;
  channel: number;
  channelName: string;
  by: string;
  started: number;
  stopped: number;
  expires: number;
  reason: string;
  size: number;
  tracks: RecordingTrack[];
};

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object";
const isNum = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
const isStr = (v: unknown): v is string => typeof v === "string";
const time = (v: unknown) => (isStr(v) ? Date.parse(v) : NaN);
const isTrack = (v: unknown): v is RecordingTrack =>
  isObj(v) && isStr(v.name) && isStr(v.file) && isNum(v.channels) && isNum(v.start);

// The server's list (times as RFC 3339 strings); null: not a valid one.
export const parseRecordings = (v: unknown): FinishedRecording[] | null => {
  if (!Array.isArray(v)) return null;
  const out: FinishedRecording[] = [];
  for (const r of v) {
    if (!isObj(r) || !isStr(r.id) || !isNum(r.channel) || !isStr(r.channelName) || !isStr(r.by) || !isStr(r.reason) || !isNum(r.size)) return null;
    if (!Array.isArray(r.tracks) || !r.tracks.every(isTrack)) return null;
    const [started, stopped, expires] = [time(r.started), time(r.stopped), time(r.expires)];
    if ([started, stopped, expires].some(Number.isNaN)) return null;
    const { id, channel, channelName, by, reason, size, tracks } = r;
    out.push({ id, channel, channelName, by, started, stopped, expires, reason, size, tracks });
  }
  return out;
};

// Admins and moderators record (as the server's isStaff), if the server
// records at all.
export const mayRecord = (server: ServerState, text: TextState) => {
  const userId = selfOf(server)?.userId ?? null;
  return text.recording !== null && (may(server, 0, Perm.Write) || roleOf(text.roles, userId) !== null);
};

export const describeRecording = (r: FinishedRecording) => ({
  length: formatDuration((r.stopped - r.started) / 1000),
  size: `${(r.size / 1024 / 1024).toFixed(1)} MB`,
  speakers: r.tracks.length ? r.tracks.map((t) => t.name).join(", ") : t("admin.recordingNobodySpoke"),
});

// The download (a token for one download, from the server).
export const recordingUrl = (base: string, id: string, token: string) =>
  `${base}/api/recordings/${encodeURIComponent(id)}?token=${encodeURIComponent(token)}`;
