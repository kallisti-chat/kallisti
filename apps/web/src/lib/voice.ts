import { safeGet, safeSet } from "./storage";

export type SelfState = { selfMute: boolean; selfDeaf: boolean };

// Same rules as the Mumble client: deaf implies mute, and unmuting while
// deaf also undeafens. Undeafening brings back the mute from before
// deafening (mutedBefore), as in Discord.
export const toggleMute = ({ selfMute, selfDeaf }: SelfState): SelfState =>
  selfMute ? { selfMute: false, selfDeaf: false } : { selfMute: true, selfDeaf };

export const toggleDeaf = ({ selfDeaf }: SelfState, mutedBefore = false): SelfState =>
  selfDeaf ? { selfMute: mutedBefore, selfDeaf: false } : { selfMute: true, selfDeaf: true };

// Self-mute and deafen, kept per server across page reloads (as Mumble
// keeps them across restarts): "muted", "deaf" or "".
type Storage = Parameters<typeof safeGet>[0];
const selfStateKey = (site: string) => `selfState:${site}`;

export const loadSelfState = (storage: Storage, site: string): SelfState => {
  const saved = safeGet(storage, selfStateKey(site));
  return { selfMute: saved === "muted" || saved === "deaf", selfDeaf: saved === "deaf" };
};

export const saveSelfState = (storage: Storage, site: string, s: SelfState) =>
  safeSet(storage, selfStateKey(site), s.selfDeaf ? "deaf" : s.selfMute ? "muted" : "");

// The microphone is open (the browser shows it recording) only while it is
// used: in a voice channel, or in the voice settings (level meter). Muted it
// stays open (nothing is sent): opening a microphone makes the browser
// reconfigure the audio device, which interrupts the sound. closeWhenMuted
// closes it anyway.
export const micWanted = (s: {
  inVoice: boolean;
  selfMute: boolean;
  settingsOpen: boolean;
  closeWhenMuted: boolean;
}): boolean => (s.inVoice && !(s.selfMute && s.closeWhenMuted)) || s.settingsOpen;

// Browsers may wait for a gesture at startup, or after an interruption.
// Use the same states for the hint and the click/key recovery path.
export const audioNeedsResume = (state: string | null) => state === "suspended" || state === "interrupted";
export const soundHint = (state: string | null, inVoice: boolean) => inVoice && audioNeedsResume(state);

// One voice channel across servers (several servers in one client): the
// one that joined voice last (in now, not in before) stays, the others
// leave, once (leaving takes the server's answer: until then both are in
// voice); null: none joined.
export const voiceJoined = (before: readonly string[], now: readonly string[]): string | null =>
  now.filter((id) => !before.includes(id)).at(-1) ?? null;
