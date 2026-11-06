// What our Opus encoder sends: speech at 40 kbps mono, or music (see
// Settings.musicMode) at 128 kbps, stereo with a two-channel microphone.
// Always 20 ms frames: the frame numbers count on that (capture.svelte.ts).
// Stereo is safe for Mumble: the channel count is in each Opus packet, and
// a mono decoder mixes stereo packets down.

export const SAMPLE_RATE = 48_000;
export const FRAME_MS = 20;

export type AudioChannels = "auto" | "mono" | "stereo";

// application and signal are newer than TypeScript's types; browsers
// without them ignore them.
type OpusConfig = OpusEncoderConfig & { application?: "voip" | "audio" | "lowdelay"; signal?: "auto" | "music" | "voice" };

export const encoderConfig = (music: boolean, channels: number, bitrate?: number): AudioEncoderConfig & { opus: OpusConfig } => ({
  codec: "opus",
  sampleRate: SAMPLE_RATE,
  numberOfChannels: channels,
  bitrate: bitrate ?? (music ? 128_000 : 40_000),
  opus: {
    frameDuration: FRAME_MS * 1000,
    ...(music && { application: "audio", signal: "music", complexity: 10 }),
  },
});

// trackChannels: from the microphone's getSettings() (some browsers do
// not say).
export const sendChannels = (music: boolean, trackChannels: number | undefined, mode: AudioChannels = "auto"): number => {
  if (mode === "mono") return 1;
  if (mode === "stereo") return (trackChannels ?? 2) >= 2 ? 2 : 1;
  return music && (trackChannels ?? 1) >= 2 ? 2 : 1;
};
