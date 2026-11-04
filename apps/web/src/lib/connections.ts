import type { Key } from "../i18n/en";
import { qualityLevel, type Level } from "./connectionQuality";
import { t } from "./i18n.svelte";

// The admin view of all connections (Server settings → Connections): what
// the server measured over the last 30 to 40 s. No addresses: admins see
// them in Mumble's user information, the table need not show them around.
export type Connection = {
  session: number;
  name: string;
  transport: string; // of its voice: "udp", "tcp" or "websocket"
  ping: number | null; // ms, as the client measured it (null: none yet)
  // Late or lost voice in percent: what the server got from the client,
  // and what the client says it got (null: too little voice).
  fromClient: number | null;
  fromServer: number | null;
  dropped: number; // voice frames it did not get: its queue was full
  overflows: number; // text messages it lost
  online: number; // seconds
  client: string;
  os: string;
  // Its voice lately, as the server got it (missing: none yet, or an older
  // server): the Opus band, stereo, frame length, bitrate of the Opus data.
  voiceBand?: string;
  voiceStereo?: boolean;
  voiceFrameMs?: number;
  voiceKbps?: number;
  // Stream frames it missed as a viewer, its connection full (since it
  // connected; older servers: missing).
  streamPictures?: number;
  streamSound?: number;
  // Its microphone, as a web client's browser says (see lib/voice/micInfo).
  micName?: string;
  micRate?: number;
  micChannels?: number;
  micFilters?: string[];
};

// The admin view asks again this often while it is open.
export const POLL_MS = 5_000;

const isNumOrNull = (v: unknown) => v === null || (typeof v === "number" && Number.isFinite(v));
const isConnection = (v: unknown): v is Connection => {
  if (!v || typeof v !== "object") return false;
  const c = v as Record<string, unknown>;
  return (
    ["session", "dropped", "overflows", "online"].every((k) => typeof c[k] === "number") &&
    ["name", "transport", "client", "os"].every((k) => typeof c[k] === "string") &&
    ["ping", "fromClient", "fromServer"].every((k) => isNumOrNull(c[k])) &&
    (c.voiceBand === undefined || typeof c.voiceBand === "string") &&
    ["voiceFrameMs", "voiceKbps", "streamPictures", "streamSound", "micRate", "micChannels"].every((k) => c[k] === undefined || typeof c[k] === "number") &&
    (c.micName === undefined || typeof c.micName === "string") &&
    (c.micFilters === undefined || (Array.isArray(c.micFilters) && c.micFilters.every((f) => typeof f === "string")))
  );
};

export const parseConnections = (v: unknown): Connection[] | null => (Array.isArray(v) ? v.filter(isConnection) : null);

// As the own connection info, by what is known: the worse direction counts.
export const connectionLevel = (c: Connection): Level | null => {
  const bad = [c.fromClient, c.fromServer].filter((p) => p !== null);
  if (c.ping === null && !bad.length) return null;
  return qualityLevel(c.ping ?? 0, bad.length ? Math.max(...bad) : null);
};

const RANK = { poor: 0, fair: 1, good: 2, unknown: 3 };

// Worst first: by level (not known yet last), then the slower ping.
export const worstFirst = (list: readonly Connection[]): Connection[] =>
  list
    .map((c) => ({ c, rank: RANK[connectionLevel(c) ?? "unknown"] }))
    .sort((a, b) => a.rank - b.rank || (b.c.ping ?? -1) - (a.c.ping ?? -1))
    .map(({ c }) => c);

export const formatPing = (ms: number | null) => (ms === null ? "–" : `${Math.round(ms)} ms`);

export const formatPercent = (p: number | null) => (p === null ? "–" : `${p < 10 ? Math.round(p * 10) / 10 : Math.round(p)} %`);

export const formatOnline = (s: number) => {
  const [d, h, m] = [Math.floor(s / 86400), Math.floor(s / 3600) % 24, Math.floor(s / 60) % 60];
  if (d) return `${t("admin.unitDays", { count: d })} ${t("admin.unitHours", { count: h })}`;
  if (h) return `${t("admin.unitHours", { count: h })} ${t("admin.unitMinutes", { count: m })}`;
  return m ? t("admin.unitMinutes", { count: m }) : t("admin.unitSeconds", { count: s });
};

const BANDS: Record<string, Key> = {
  narrow: "admin.soundBandNarrow",
  medium: "admin.soundBandMedium",
  wide: "admin.soundBandWide",
  "super-wide": "admin.soundBandSuperWide",
  full: "admin.soundBandFull",
};
const bandText = (band: string) => (BANDS[band] ? t(BANDS[band]) : t("admin.soundBandOther", { band }));

// A speaker's sound for the admin view. Narrow and medium band sound
// muffled: often a Bluetooth headset whose microphone is in its phone mode.
export const formatSound = (c: Pick<Connection, "voiceBand" | "voiceKbps" | "voiceStereo">) =>
  c.voiceBand
    ? {
        text: [bandText(c.voiceBand), ...(c.voiceStereo ? [t("admin.stereo")] : []), `${Math.round(c.voiceKbps ?? 0)} kbps`].join(" · "),
        muffled: c.voiceBand === "narrow" || c.voiceBand === "medium",
      }
    : { text: "–", muffled: false };

// A web speaker's microphone for the admin view. 16 kHz or less: a
// Bluetooth headset in its phone mode (or a phone-grade microphone).
export const formatMic = (c: Pick<Connection, "micName" | "micRate" | "micChannels" | "micFilters">) =>
  c.micName === undefined
    ? { text: "–", filters: "", phoneMode: false }
    : {
        text: [
          c.micName || t("admin.micUnnamed"),
          ...(c.micRate ? [`${+(c.micRate / 1000).toFixed(1)} kHz`] : []),
          ...(c.micChannels ? [c.micChannels === 1 ? t("admin.mono") : c.micChannels === 2 ? t("admin.stereo") : t("admin.audioChannels", { count: c.micChannels })] : []),
        ].join(" · "),
        filters: c.micFilters?.length ? c.micFilters.join(", ") : t("admin.micNoFilters"),
        phoneMode: !!c.micRate && c.micRate <= 16_000,
      };
