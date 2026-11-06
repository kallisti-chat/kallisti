import { t } from "./i18n.svelte";
import { DEFAULT_QUALITY, parseQuality, type Kind, type Quality } from "./stream/quality";
import type { TimeFormat } from "./chat";
import type { PlaybackBuffer } from "./voice/buffer";
import type { NoiseMode } from "./voice/noise";
import type { AudioChannels } from "./voice/opus";
import type { TransmitMode } from "./voice/vad";

export type Settings = {
  mode: TransmitMode;
  pttKey: string; // KeyboardEvent.code
  vadThresholdDb: number;
  inputDeviceId: string | null;
  outputDeviceId: string | null;
  // Desktop notifications for mentions and private messages.
  notifications: boolean;
  // Close the microphone while muted (the browser stops showing it as
  // recording, but opening it again interrupts the sound for a moment).
  closeMicWhenMuted: boolean;
  // Others see a thumbnail of our screen share or camera before they watch.
  streamPreviews: boolean;
  // See voice/noise.ts.
  noiseSuppression: NoiseMode;
  // Colours: "system" follows the system's light or dark setting.
  theme: ThemeSetting;
  // Message times: 12 or 24 hours, or as the language has it.
  timeFormat: TimeFormat;
  // The browser's echo cancellation. Off with a headset: in WebKit (the
  // desktop app, Safari) on macOS it puts Bluetooth headsets into their
  // call mode, also with another microphone chosen.
  echoCancellation: boolean;
  // Automatic gain control in normal mode.
  autoGainControl: boolean;
  // Audio channels: "auto" (mono for voice, stereo for music if available), "mono", "stereo".
  audioChannels: AudioChannels;
  // Bitrate in bps for voice and music modes.
  voiceBitrate: number;
  musicBitrate: number;
  // For music: none of the filters above, no automatic gain, more bits
  // and stereo (see voice/opus.ts).
  musicMode: boolean;
  // For every speaker (see voice/buffer.ts).
  playbackBuffer: PlaybackBuffer;
  // The camera to stream (null: the default), and the quality of each kind
  // of stream (also changed while live).
  cameraDeviceId: string | null;
  streamQuality: Record<Kind, Quality>;
  // Mute and deafen sounds (see sounds.ts), volume 0 to 1.
  sounds: boolean;
  soundVolume: number;
  // Translate with the larger model (see translate/llm.ts): a download of
  // about 1.4 GB, WebGPU only.
  betterTranslation: boolean;
};

// What to ask the browser for: exactly the chosen microphone (null: the
// default), with its own filters as set (browserNoise: its noise filter,
// when ours are off). Music: no filters, both channels if it has two.
export const micConstraints = (
  deviceId: string | null,
  o: {
    browserNoise: boolean;
    cancelEcho: boolean;
    autoGainControl?: boolean;
    music: boolean;
    channels?: AudioChannels;
    audioChannels?: AudioChannels;
  },
): MediaTrackConstraints => {
  const ch = o.audioChannels ?? o.channels;
  const wantStereo = ch === "stereo" || (ch !== "mono" && o.music);
  return {
    deviceId: deviceId ? { exact: deviceId } : undefined,
    echoCancellation: o.cancelEcho && !o.music,
    noiseSuppression: o.browserNoise && !o.music,
    autoGainControl: (o.autoGainControl ?? true) && !o.music,
    ...(wantStereo && { channelCount: { ideal: 2 } }),
    ...(ch === "mono" && { channelCount: { ideal: 1 } }),
  };
};

export type ThemeSetting = "dark" | "light" | "system";
export const themeOf = (setting: ThemeSetting, systemDark: boolean): "dark" | "light" =>
  setting === "system" ? (systemDark ? "dark" : "light") : setting;

export const METER_MIN_DB = -80;

export const DEFAULT_SETTINGS: Settings = {
  mode: "vad",
  pttKey: "Backquote",
  vadThresholdDb: -45,
  inputDeviceId: null,
  outputDeviceId: null,
  notifications: false,
  closeMicWhenMuted: false,
  streamPreviews: true,
  noiseSuppression: "browser",
  theme: "dark",
  timeFormat: "auto",
  echoCancellation: true,
  autoGainControl: true,
  audioChannels: "auto",
  voiceBitrate: 40_000,
  musicBitrate: 128_000,
  musicMode: false,
  playbackBuffer: "low",
  cameraDeviceId: null,
  streamQuality: DEFAULT_QUALITY,
  sounds: true,
  soundVolume: 0.5,
  betterTranslation: false,
};

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n));
const str = (v: unknown) => (typeof v === "string" && v ? v : null);

// Stored data can be old or broken: keep what is valid, default the rest.
export const parseSettings = (raw: string | null): Settings => {
  let data: unknown;
  try {
    data = JSON.parse(raw ?? "null");
  } catch {
    return DEFAULT_SETTINGS;
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) return DEFAULT_SETTINGS;
  const d = data as Record<string, unknown>;
  const db = d.vadThresholdDb;
  const quality = (d.streamQuality ?? {}) as Record<string, unknown>;
  return {
    mode: d.mode === "vad" || d.mode === "ptt" ? d.mode : DEFAULT_SETTINGS.mode,
    pttKey: str(d.pttKey) ?? DEFAULT_SETTINGS.pttKey,
    vadThresholdDb:
      typeof db === "number" && db >= -120
        ? clamp(db, METER_MIN_DB, 0)
        : DEFAULT_SETTINGS.vadThresholdDb,
    inputDeviceId: str(d.inputDeviceId),
    outputDeviceId: str(d.outputDeviceId),
    notifications: d.notifications === true,
    closeMicWhenMuted: d.closeMicWhenMuted === true,
    streamPreviews: d.streamPreviews !== false,
    noiseSuppression:
      d.noiseSuppression === "off" || d.noiseSuppression === "strong" || d.noiseSuppression === "strongest"
        ? d.noiseSuppression
        : "browser",
    theme: d.theme === "light" || d.theme === "system" ? d.theme : "dark",
    timeFormat: d.timeFormat === "12" || d.timeFormat === "24" ? d.timeFormat : "auto",
    echoCancellation: d.echoCancellation !== false,
    autoGainControl: d.autoGainControl !== false,
    audioChannels: d.audioChannels === "mono" || d.audioChannels === "stereo" ? d.audioChannels : "auto",
    voiceBitrate:
      typeof d.voiceBitrate === "number" && d.voiceBitrate >= 16_000
        ? clamp(d.voiceBitrate, 16_000, 558_000)
        : DEFAULT_SETTINGS.voiceBitrate,
    musicBitrate:
      typeof d.musicBitrate === "number" && d.musicBitrate >= 32_000
        ? clamp(d.musicBitrate, 32_000, 558_000)
        : DEFAULT_SETTINGS.musicBitrate,
    musicMode: d.musicMode === true,
    playbackBuffer:
      typeof d.playbackBuffer === "number" && !isNaN(d.playbackBuffer)
        ? clamp(Math.round(d.playbackBuffer), 30, 500)
        : d.playbackBuffer === "medium" || d.playbackBuffer === "high"
          ? d.playbackBuffer
          : "low",
    cameraDeviceId: str(d.cameraDeviceId),
    streamQuality: { screen: parseQuality(quality.screen, "screen"), camera: parseQuality(quality.camera, "camera") },
    sounds: d.sounds !== false,
    soundVolume: typeof d.soundVolume === "number" ? clamp(d.soundVolume, 0, 1) : DEFAULT_SETTINGS.soundVolume,
    betterTranslation: d.betterTranslation === true,
  };
};

// Push-to-talk works only while the page has focus (a browser limit).
// Typing in a text field never presses the key, but release always counts,
// so the key cannot get stuck.
export const pttAction = (
  type: "keydown" | "keyup",
  ev: { code: string; repeat: boolean; editable: boolean },
  pttKey: string,
): "down" | "up" | null => {
  if (ev.code !== pttKey) return null;
  if (type === "keyup") return "up";
  return ev.repeat || ev.editable ? null : "down";
};

// The PTT key must not also do its normal job, for example Space pressing a
// focused button (which happens on keyup). Text fields keep the key.
export const pttBlocksDefault = (ev: { code: string; repeat: boolean; editable: boolean }, pttKey: string) =>
  ev.code === pttKey && !ev.editable;

const KEY_NAMES: Record<string, () => string> = {
  Backquote: () => "`",
  Space: () => t("settings.keySpace"),
  CapsLock: () => t("settings.keyCapsLock"),
};

export const keyLabel = (code: string): string => {
  const named = KEY_NAMES[code];
  if (named) return named();
  const m = /^(Key|Digit)(.)$/.exec(code);
  if (m) return m[2]!;
  const side = /^(Shift|Control|Alt|Meta)(Left|Right)$/.exec(code);
  if (side) return t(side[2] === "Left" ? "settings.keyLeft" : "settings.keyRight", { key: side[1]! });
  return code;
};

export const meterPercent = (db: number) =>
  Math.round(clamp(((db - METER_MIN_DB) / -METER_MIN_DB) * 100, 0, 100));
