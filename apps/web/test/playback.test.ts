import { describe, expect, it, vi } from "vitest";
import type { AudioPacket } from "@kallisti/protocol";
import { createPlayback } from "../src/lib/voice/playback.svelte";

// Decoders that only count what they get.
const decoded = vi.hoisted(() => [] as Uint8Array[]);
vi.mock("../src/lib/voice/codec", () => ({
  voiceDecoder: () => ({ decode: (opus: Uint8Array) => decoded.push(opus), close: () => {} }),
}));
vi.mock("../src/lib/voice/limiter", () => ({ createLimiter: () => ({ input: {}, output: { connect: () => {} } }) }));
vi.mock("../src/lib/voice/output", () => ({ voiceOutput: () => ({ input: {} }) }));

const gainNode = () => ({ gain: { value: 1, setTargetAtTime: vi.fn() }, connect: vi.fn(), disconnect: vi.fn() });
const setup = () => {
  decoded.length = 0;
  const gains: ReturnType<typeof gainNode>[] = [];
  const ctx = {
    currentTime: 0,
    destination: {},
    createGain: () => {
      const g = gainNode();
      gains.push(g);
      return g;
    },
  } as unknown as AudioContext;
  return { playback: createPlayback(ctx), gains };
};
const packet = (session: number): AudioPacket => ({ session, frameNumber: 1, opus: new Uint8Array([1]), terminator: false }) as AudioPacket;

describe("playback", () => {
  it("drops voice of a speaker whose audio settings are not in yet (voice may overtake their UserState)", () => {
    const { playback } = setup();
    playback.handle(packet(5));
    expect(decoded).toHaveLength(0);
    expect(playback.speaking.has(5)).toBe(false);
  });

  it("plays a known speaker at their volume, and not a locally muted one", () => {
    const { playback, gains } = setup();
    playback.setUserGain(5, 0.5);
    playback.setUserGain(6, 0);
    playback.handle(packet(5));
    playback.handle(packet(6));
    expect(decoded).toHaveLength(1);
    expect(gains.at(-2)!.gain.value).toBe(0.5); // the output, then one per speaker
    expect(playback.speaking.has(6)).toBe(true); // muted still shows the ring
  });

  it("knows who whispers or shouts to me while they talk", () => {
    const { playback } = setup();
    playback.setUserGain(5, 1);
    playback.handle({ ...packet(5), target: 2 });
    expect(playback.heard.get(5)).toBe("whisper");
    playback.handle({ ...packet(5), target: 0 }); // back to normal talk
    expect(playback.heard.has(5)).toBe(false);
    playback.handle({ ...packet(5), target: 1 });
    expect(playback.heard.get(5)).toBe("shout");
    playback.handle({ ...packet(5), target: 1, terminator: true });
    expect(playback.heard.has(5)).toBe(false);
  });

  it("forgets a speaker who left: their next session waits for its settings again", () => {
    const { playback } = setup();
    playback.setUserGain(5, 1);
    playback.forget(5);
    playback.handle(packet(5));
    expect(decoded).toHaveLength(0);
  });
});
