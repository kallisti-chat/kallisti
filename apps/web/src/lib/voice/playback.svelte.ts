import type { AudioPacket } from "@kallisti/protocol";
import { SvelteMap, SvelteSet } from "svelte/reactivity";
import { createLimiter } from "./limiter";
import { playoutFor, type PlaybackBuffer } from "./buffer";
import { createMusicDetector, trackMusic, type MusicDetector } from "./music";
import { chunkOutcome, nextStart, startsAnew, type PlayoutConfig } from "./schedule";
import type { Counters } from "../connectionQuality";
import { voiceDecoder, type VoiceDecoder } from "./codec";
import { voiceOutput } from "./output";
import { heardAs, type Heard } from "../whisper";

const SAMPLE_RATE = 48_000;
// Mumble counts frames in 10 ms steps.
const FRAME_US = 10_000;
const SPEAKING_TIMEOUT_MS = 250;

type Speaker = {
  session: number;
  decoder: VoiceDecoder;
  gain: GainNode;
  end: number | null;
  // The playout timing of the current stream (see playoutFor).
  cfg: PlayoutConfig;
  music: MusicDetector;
  musicStream: boolean; // the current stream has the music buffer
  // Where the stream goes on (µs, null: a new talk spurt), and for each
  // chunk in the decoder (outputs come in order) its frame time and whether
  // it ends a spurt (see chunkOutcome). Not the output's timestamp: a
  // decoder may count those itself.
  next: number | null;
  queued: { at: number; end: boolean }[];
  timer?: ReturnType<typeof setTimeout>;
};

export type Playback = ReturnType<typeof createPlayback>;

export const createPlayback = (ctx: AudioContext) => {
  const speakers = new Map<number, Speaker>();
  // Per-user volume (0 = locally muted). Kept apart from `speakers`, which
  // come and go with each talk spurt's decoder. No entry: the client does
  // not know the speaker yet (voice can overtake their UserState), or not
  // their local mute and volume: their voice is dropped.
  const gains = new Map<number, number>();
  const speaking = new SvelteSet<number>();
  // Who of them whispers or shouts to me now (see lib/whisper).
  const heard = new SvelteMap<number, Heard>();
  const silent = (session: number) => (heard.delete(session), speaking.delete(session));
  const output = ctx.createGain();
  const limiter = createLimiter(ctx);
  output.connect(limiter.input);
  const out = voiceOutput(ctx);
  limiter.output.connect(out.input);
  let deaf = false;
  let delaySetting: PlaybackBuffer = "low";
  // Who is in a listen-only channel: they get the music buffer.
  let listenOnly: ReadonlySet<number> = new Set();
  // For the connection info (counters only: this is the audio path).
  const counters: Counters = { good: 0, late: 0, lost: 0 };

  // planes: one per channel (see VoiceDecoder).
  const play = (speaker: Speaker, planes: Float32Array<ArrayBuffer>[], timestamp: number) => {
    const now = ctx.currentTime * SAMPLE_RATE;
    // A changed buffer (setting, music) applies from the next start: never
    // cut a stream.
    if (startsAnew(speaker.end, now)) {
      speaker.musicStream = speaker.music.music || listenOnly.has(speaker.session);
      speaker.cfg = playoutFor(delaySetting, speaker.musicStream);
    }
    const start = nextStart(speaker.end, now, speaker.cfg);
    const frames = planes[0]?.length ?? 0;
    const duration = (frames / SAMPLE_RATE) * 1e6;
    const chunk = speaker.queued.shift() ?? { at: timestamp, end: false };
    const { late, lost } = chunkOutcome({ expected: speaker.next, timestamp: chunk.at, duration, start, prevEnd: speaker.end, now });
    counters[late ? "late" : "good"]++;
    if (late && speaker.musicStream) counters.musicLate = (counters.musicLate ?? 0) + 1;
    counters.lost += lost;
    speaker.next = chunk.end ? null : chunk.at + duration;
    if (start === null || !frames) return;
    // Stereo streams (music bots) keep both channels; voice is mono in
    // both.
    const buffer = ctx.createBuffer(planes.length, frames, SAMPLE_RATE);
    planes.forEach((plane, ch) => buffer.copyToChannel(plane, ch));
    const source = ctx.createBufferSource();
    source.buffer = buffer;
    source.connect(speaker.gain);
    source.start(start / SAMPLE_RATE);
    speaker.end = start + frames;
  };

  const remove = (session: number) => {
    const speaker = speakers.get(session);
    if (!speaker) return;
    clearTimeout(speaker.timer);
    speaker.decoder.close();
    speaker.gain.disconnect();
    speakers.delete(session);
    silent(session);
  };

  const speakerFor = (session: number): Speaker => {
    const existing = speakers.get(session);
    if (existing) return existing;
    const gain = ctx.createGain();
    gain.gain.value = gains.get(session)!;
    gain.connect(output);
    const speaker: Speaker = {
      session,
      gain,
      end: null,
      cfg: playoutFor(delaySetting, false),
      music: createMusicDetector(),
      musicStream: false,
      next: null,
      queued: [],
      // Two channels: stereo packets keep their image, mono ones come out
      // the same on both sides.
      decoder: voiceDecoder({
        output: (planes, timestamp) => play(speaker, planes, timestamp),
        // A broken stream: drop the decoder, the next packet makes a new one.
        error: (err) => (console.warn("decoder error", session, err), remove(session)),
      }),
    };
    speakers.set(session, speaker);
    return speaker;
  };

  const markSpeaking = (session: number, speaker: Speaker, terminator: boolean, context: number) => {
    clearTimeout(speaker.timer);
    if (terminator) return silent(session);
    speaking.add(session);
    const as = heardAs(context);
    if (!as) heard.delete(session);
    else if (heard.get(session) !== as) heard.set(session, as);
    speaker.timer = setTimeout(() => silent(session), SPEAKING_TIMEOUT_MS);
  };

  return {
    speaking: speaking as ReadonlySet<number>,
    heard: heard as ReadonlyMap<number, Heard>,
    // Must not throw: it runs inside the control connection's message handler.
    handle: (packet: AudioPacket) => {
      if (deaf || !gains.has(packet.session)) return;
      try {
        const speaker = speakerFor(packet.session);
        markSpeaking(packet.session, speaker, packet.terminator, packet.target);
        // Locally muted: keep the speaking ring, skip the decoding work.
        if (gains.get(packet.session) === 0) return;
        const at = packet.frameNumber * FRAME_US;
        if (packet.opus.length === 0) {
          // A bare terminator ends the spurt after the chunks in the decoder.
          const last = speaker.queued.at(-1);
          if (packet.terminator && last) last.end = true;
          else if (packet.terminator) speaker.next = null;
          return;
        }
        trackMusic(speaker.music, packet.opus);
        speaker.queued.push({ at, end: packet.terminator });
        speaker.decoder.decode(packet.opus, at);
      } catch (err) {
        // Decoder failures go to its error callback; this is for anything
        // else. Drop the decoder, the next packet makes a new one.
        console.warn("voice packet dropped", packet.session, err);
        remove(packet.session);
      }
    },
    remove,
    stats: (): Counters => ({ ...counters }),
    // The user left: also drop their volume.
    forget: (session: number) => {
      remove(session);
      gains.delete(session);
    },
    setUserGain: (session: number, value: number) => {
      // Unmuted: the decoder skipped packets meanwhile, so start a new one.
      if (gains.get(session) === 0 && value > 0) remove(session);
      gains.set(session, value);
      const speaker = speakers.get(session);
      // A short ramp: a jump in gain can click.
      speaker?.gain.gain.setTargetAtTime(value, ctx.currentTime, 0.02);
    },
    // Does nothing where outputDeviceSupported() is false.
    setOutputDevice: out.setSinkId,
    // The background media session (see voice/output.ts).
    setVoice: out.setVoice,
    releaseVoice: out.release,
    setBuffer: (value: PlaybackBuffer) => {
      delaySetting = value;
    },
    setListenOnly: (sessions: ReadonlySet<number>) => {
      listenOnly = sessions;
    },
    setDeaf: (value: boolean) => {
      deaf = value;
      // Also silences chunks that are already scheduled.
      output.gain.value = deaf ? 0 : 1;
      if (deaf) [...speakers.keys()].forEach(remove);
    },
    close: () => [...speakers.keys()].forEach(remove),
  };
};
