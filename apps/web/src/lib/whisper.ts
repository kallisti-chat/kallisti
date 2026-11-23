import { Perm, TEXT_ONLY, type OutgoingAudio } from "@kallisti/protocol";

// Whisper and shout (Mumble's voice targets): my voice goes only to a
// user, or to everyone in a channel, while it is on. One target is enough
// for a mode that is turned on and off.
export type WhisperTarget = { session: number } | { channel: number };
export const WHISPER_TARGET = 1;

export const sameTarget = (a: WhisperTarget | null, b: WhisperTarget | null) =>
  !!a && !!b && ("session" in a ? "session" in b && a.session === b.session : "channel" in b && a.channel === b.channel);

// The target of each voice packet for the server. A whisper that ends in
// the middle of a talk spurt (its user left, or "stop") keeps the spurt
// private to its end: the server has no target for it then, so the rest
// of the sentence goes to nobody, not to the channel.
export const createWhisperTargets = () => {
  let to: WhisperTarget | null = null;
  let talking = false;
  let hold = false;
  return {
    set: (next: WhisperTarget | null) => {
      hold = !next && (hold || (!!to && talking));
      to = next;
    },
    stamp: (packet: OutgoingAudio): OutgoingAudio => {
      const whisper = !!to || hold;
      talking = !packet.terminator;
      if (packet.terminator) hold = false;
      return whisper ? { ...packet, target: WHISPER_TARGET } : packet;
    },
  };
};

// A whisper ends when its user leaves voice or its channel goes.
export const whisperHolds = (
  whisper: WhisperTarget,
  server: { users: Readonly<Record<number, { channelId: number } | undefined>>; channels: Readonly<Record<number, unknown>> },
) => {
  if ("channel" in whisper) return whisper.channel in server.channels;
  const user = server.users[whisper.session];
  return !!user && user.channelId !== TEXT_ONLY;
};

// My voice gets through to a channel (a shout into it, a whisper to a
// user in it): the server drops it without a word when I lack Whisper
// there. A shout also needs Speak: not into listen-only channels.
// Permissions not asked for yet: offered, the server decides.
export const mayReach = (permissions: number | null | undefined, shout: boolean) => {
  const need = shout ? Perm.Whisper | Perm.Speak : Perm.Whisper;
  return permissions === null || permissions === undefined || (permissions & need) === need;
};

// How a voice packet came to me, from its context (see protocol's
// AudioPacket): null for normal talk.
export type Heard = "whisper" | "shout";
export const heardAs = (context: number): Heard | null => (context === 2 ? "whisper" : context === 1 ? "shout" : null);

// While a priority speaker talks, the others are quieter (as in Mumble's
// client, which does this itself).
const DUCK = 0.3;
export const duckedGain = (gain: number, session: number, speaking: ReadonlySet<number>, priority: ReadonlySet<number>) =>
  !priority.has(session) && [...priority].some((p) => speaking.has(p)) ? gain * DUCK : gain;
