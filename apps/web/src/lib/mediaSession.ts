// Android freezes pages in the background (another app, the screen off),
// and the call drops. Pages that play media with a Media Session are kept
// running: in voice, the voice plays through an <audio> element (see
// voice/output.ts) and the system shows it as playing media.

import { t } from "./i18n.svelte";

export type VoiceSessionAction = "leave" | "deafen";

// Pause, stop and a headset's hang-up button: leave voice; plain Mumble
// servers have no place outside voice, so deafen there.
export const voiceSession = (s: { channel: string; server: string; lobby: boolean }) => {
  const action: VoiceSessionAction = s.lobby ? "leave" : "deafen";
  return {
    metadata: {
      title: t("media.voiceSessionTitle", { channel: s.channel }),
      artist: s.server,
      artwork: [192, 512].map((n) => ({ src: `/icons/icon-${n}.png`, sizes: `${n}x${n}`, type: "image/png" })),
    },
    actions: { pause: action, stop: action, hangup: action } satisfies Partial<Record<MediaSessionAction | "hangup", VoiceSessionAction>>,
  };
};

// Only on Android: desktops do not freeze pages, the extra element adds a
// little latency, and there the media keys (play/pause for another
// player) would leave voice. iOS suspends PWAs anyway and has known
// trouble with stream-fed <audio> while the microphone is open.
export const backgroundAudioWanted = (env: { android: boolean; mediaSession: boolean; mediaStreams: boolean }) =>
  env.android && env.mediaSession && env.mediaStreams;
