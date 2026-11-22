import { describe, expect, it } from "vitest";
import { createWhisperTargets, duckedGain, heardAs, mayReach, sameTarget, WHISPER_TARGET, whisperHolds } from "../src/lib/whisper";

const TEXT_ONLY = 0xfffffffe;

describe("whisper", () => {
  const packet = (terminator = false) => ({ target: 0, frameNumber: 1, opus: new Uint8Array(), terminator });

  it("sends voice to the target while a whisper is on", () => {
    const targets = createWhisperTargets();
    expect(targets.stamp(packet()).target).toBe(0);
    targets.set({ session: 4 });
    expect(targets.stamp(packet()).target).toBe(WHISPER_TARGET);
    targets.set({ channel: 2 });
    expect(targets.stamp(packet()).target).toBe(WHISPER_TARGET);
  });

  it("keeps a talk spurt private to its end when the whisper ends in it", () => {
    const targets = createWhisperTargets();
    targets.set({ session: 4 });
    targets.stamp(packet()); // talking
    targets.set(null); // the user left, or "stop"
    expect(targets.stamp(packet()).target).toBe(WHISPER_TARGET); // the rest of the sentence: to nobody
    expect(targets.stamp(packet(true)).target).toBe(WHISPER_TARGET);
    expect(targets.stamp(packet()).target).toBe(0); // the next spurt: the channel
    targets.stamp(packet(true));
    // Not talking when it ends: the next spurt is normal at once.
    targets.set({ session: 4 });
    targets.set(null);
    expect(targets.stamp(packet()).target).toBe(0);
  });

  it("holds while its user is in voice or its channel exists", () => {
    const server = { users: { 4: { channelId: 1 }, 6: { channelId: TEXT_ONLY } }, channels: { 2: {} } };
    expect(whisperHolds({ session: 4 }, server)).toBe(true);
    expect(whisperHolds({ session: 5 }, server)).toBe(false);
    expect(whisperHolds({ session: 6 }, server)).toBe(false); // text only: hears nothing
    expect(whisperHolds({ channel: 2 }, server)).toBe(true);
    expect(whisperHolds({ channel: 3 }, server)).toBe(false);
  });

  it("is offered where the server lets my voice through", () => {
    const WHISPER = 0x100, SPEAK = 0x8;
    expect(mayReach(undefined, false)).toBe(true); // not asked yet: the server decides
    expect(mayReach(WHISPER, false)).toBe(true);
    expect(mayReach(SPEAK, false)).toBe(false);
    expect(mayReach(WHISPER, true)).toBe(false); // a shout into a listen-only channel
    expect(mayReach(WHISPER | SPEAK, true)).toBe(true);
  });

  it("compares targets", () => {
    expect(sameTarget({ session: 4 }, { session: 4 })).toBe(true);
    expect(sameTarget({ session: 4 }, { channel: 4 })).toBe(false);
    expect(sameTarget(null, { channel: 4 })).toBe(false);
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
