import type { OutgoingAudio } from "@kallisti/protocol";

// Whisper and shout (Mumble's voice targets): my voice goes only to a
// user, or to everyone in a channel, while it is on. One target is enough
// for a mode that is turned on and off.
export type WhisperTarget = { session: number } | { channel: number };
export const WHISPER_TARGET = 1;

// A voice packet for the server: to the target while a whisper is on.
export const withTarget = (packet: OutgoingAudio, whisper: WhisperTarget | null): OutgoingAudio =>
  whisper ? { ...packet, target: WHISPER_TARGET } : packet;

// A whisper ends when its user leaves or its channel goes.
export const whisperHolds = (
  whisper: WhisperTarget,
  server: { users: Readonly<Record<number, unknown>>; channels: Readonly<Record<number, unknown>> },
) => ("session" in whisper ? whisper.session in server.users : whisper.channel in server.channels);

// How a voice packet came to me, from its context (see protocol's
// AudioPacket): null for normal talk.
export type Heard = "whisper" | "shout";
export const heardAs = (context: number): Heard | null => (context === 2 ? "whisper" : context === 1 ? "shout" : null);

// While a priority speaker talks, the others are quieter (as in Mumble's
// client, which does this itself).
const DUCK = 0.3;
export const duckedGain = (gain: number, session: number, speaking: ReadonlySet<number>, priority: ReadonlySet<number>) =>
  !priority.has(session) && [...priority].some((p) => speaking.has(p)) ? gain * DUCK : gain;
