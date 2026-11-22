import { describe, expect, it } from "vitest";
import { duckedGain, heardAs, WHISPER_TARGET, whisperHolds, withTarget } from "../src/lib/whisper";

describe("whisper", () => {
  it("sends voice to the target while a whisper is on", () => {
    const packet = { target: 0, frameNumber: 1, opus: new Uint8Array(), terminator: false };
    expect(withTarget(packet, null).target).toBe(0);
    expect(withTarget(packet, { session: 4 }).target).toBe(WHISPER_TARGET);
    expect(withTarget(packet, { channel: 2 }).target).toBe(WHISPER_TARGET);
  });

  it("holds while its user is there or its channel exists", () => {
    const server = { users: { 4: {} }, channels: { 2: {} } };
    expect(whisperHolds({ session: 4 }, server)).toBe(true);
    expect(whisperHolds({ session: 5 }, server)).toBe(false);
    expect(whisperHolds({ channel: 2 }, server)).toBe(true);
    expect(whisperHolds({ channel: 3 }, server)).toBe(false);
  });

  it("names how a voice packet was sent to me", () => {
    expect(heardAs(0)).toBe(null);
    expect(heardAs(1)).toBe("shout");
    expect(heardAs(2)).toBe("whisper");
    expect(heardAs(3)).toBe(null); // as a listener of a channel
  });

  it("makes the others quieter while a priority speaker talks", () => {
    const priority = new Set([7]);
    expect(duckedGain(1, 3, new Set([3, 7]), priority)).toBeLessThan(0.5);
    expect(duckedGain(1, 7, new Set([3, 7]), priority)).toBe(1); // the priority speaker
    expect(duckedGain(1, 3, new Set([3]), priority)).toBe(1); // nobody with priority talks
    expect(duckedGain(0, 3, new Set([3, 7]), priority)).toBe(0); // muted stays muted
  });
});
