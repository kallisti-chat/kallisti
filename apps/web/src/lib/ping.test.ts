import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const deferred = () => {
  let resolve!: () => void;
  const promise = new Promise<void>((yes) => { resolve = yes; });
  return { promise, resolve };
};

const mockOscillator = () => ({
  frequency: { value: 0 },
  connect: vi.fn((gain: unknown) => gain),
  disconnect: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  onended: null as (() => void) | null,
});
const mockGain = () => ({
  gain: { setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() },
  connect: vi.fn(),
  disconnect: vi.fn(),
});

class MockAudioContext {
  currentTime = 10;
  destination = {};
  resume = vi.fn(() => Promise.resolve());
  suspend = vi.fn(() => Promise.resolve());
  setSinkId = vi.fn(() => Promise.resolve());
  oscillators: ReturnType<typeof mockOscillator>[] = [];
  gains: ReturnType<typeof mockGain>[] = [];

  createOscillator() {
    const oscillator = mockOscillator();
    this.oscillators.push(oscillator);
    return oscillator;
  }

  createGain() {
    const gain = mockGain();
    this.gains.push(gain);
    return gain;
  }
}

let audio: MockAudioContext;
let createContext: ReturnType<typeof vi.fn>;
let playNotes: typeof import("./ping")["playNotes"];
const flush = async () => { await Promise.resolve(); await Promise.resolve(); };

beforeEach(async () => {
  vi.resetModules();
  audio = new MockAudioContext();
  createContext = vi.fn(function () { return audio; });
  vi.stubGlobal("AudioContext", createContext);
  ({ playNotes } = await import("./ping"));
});

afterEach(() => vi.unstubAllGlobals());

describe("notification audio idle suspension", () => {
  it("disconnects each finished note and suspends only after the last one ends", async () => {
    playNotes("speakers", [880, 1320], 0.09, 0.15);
    await flush();

    expect(audio.setSinkId).toHaveBeenCalledWith("speakers");
    expect(audio.oscillators[1]!.start).toHaveBeenCalledWith(10.09);
    expect(audio.oscillators[1]!.stop).toHaveBeenCalledWith(10.29);
    expect(audio.suspend).not.toHaveBeenCalled();

    // End out of order: cleanup must count live notes, not their array index.
    audio.oscillators[1]!.onended?.();
    expect(audio.oscillators[1]!.disconnect).toHaveBeenCalledOnce();
    expect(audio.gains[1]!.disconnect).toHaveBeenCalledOnce();
    expect(audio.suspend).not.toHaveBeenCalled();
    audio.oscillators[0]!.onended?.();
    expect(audio.oscillators[0]!.disconnect).toHaveBeenCalledOnce();
    expect(audio.gains[0]!.disconnect).toHaveBeenCalledOnce();
    expect(audio.suspend).toHaveBeenCalledOnce();
  });

  it("keeps the context running while another call still has notes", async () => {
    playNotes(null, [880], 0.09, 0.15);
    playNotes(null, [1320], 0.09, 0.15);
    await flush();

    audio.oscillators[0]!.onended?.();
    expect(audio.suspend).not.toHaveBeenCalled();
    audio.oscillators[1]!.onended?.();
    expect(audio.suspend).toHaveBeenCalledOnce();
    expect(createContext).toHaveBeenCalledOnce();
  });

  it("waits for resume and schedules later notes from the resumed clock", async () => {
    playNotes(null, [880], 0.09, 0.15);
    await flush();
    audio.oscillators[0]!.onended?.();
    await flush();

    const resumed = deferred();
    audio.resume.mockReturnValueOnce(resumed.promise);
    playNotes(null, [880, 1320], 0.09, 0.15);
    expect(audio.oscillators).toHaveLength(1);
    audio.currentTime = 40;
    resumed.resolve();
    await flush();

    expect(audio.resume).toHaveBeenCalledTimes(2);
    expect(audio.oscillators[1]!.start).toHaveBeenCalledWith(40);
    expect(audio.oscillators[2]!.start).toHaveBeenCalledWith(40.09);
    expect(audio.gains[1]!.gain.setValueAtTime).toHaveBeenCalledWith(0.0001, 40);
    expect(createContext).toHaveBeenCalledOnce();
  });

  it("does not suspend while an overlapping call is still resuming", async () => {
    playNotes(null, [880], 0.09, 0.15);
    await flush();
    const resumed = deferred();
    audio.resume.mockReturnValueOnce(resumed.promise);
    playNotes(null, [1320], 0.09, 0.15);
    audio.oscillators[0]!.onended?.();
    expect(audio.suspend).not.toHaveBeenCalled();

    resumed.resolve();
    await flush();
    audio.oscillators[1]!.onended?.();
    expect(audio.suspend).toHaveBeenCalledOnce();
  });

  it("requests resume even when the previous suspension is still pending", async () => {
    const suspended = deferred();
    audio.suspend.mockReturnValueOnce(suspended.promise);
    playNotes(null, [880], 0.09, 0.15);
    await flush();
    audio.oscillators[0]!.onended?.();
    expect(audio.suspend).toHaveBeenCalledOnce();

    const resumed = deferred();
    audio.resume.mockReturnValueOnce(resumed.promise);
    playNotes(null, [1320], 0.09, 0.15);
    expect(audio.resume).toHaveBeenCalledTimes(2);
    expect(audio.oscillators).toHaveLength(1);
    suspended.resolve();
    resumed.resolve();
    await flush();
    expect(audio.oscillators).toHaveLength(2);
  });

  it("handles resume failure without scheduling sound and can retry", async () => {
    audio.resume.mockRejectedValueOnce(new Error("resume denied"));
    playNotes(null, [880], 0.09, 0.15);
    await flush();
    expect(audio.oscillators).toHaveLength(0);

    audio.suspend.mockClear();
    playNotes(null, [1320], 0.09, 0.15);
    await flush();
    expect(audio.oscillators).toHaveLength(1);
    audio.oscillators[0]!.onended?.();
    expect(audio.suspend).toHaveBeenCalledOnce();
  });

  it("handles suspension failure without an unhandled rejection", async () => {
    audio.suspend.mockRejectedValueOnce(new Error("suspend denied"));
    playNotes(null, [880], 0.09, 0.15);
    await flush();
    audio.oscillators[0]!.onended?.();
    await flush();
    expect(audio.suspend).toHaveBeenCalledOnce();
  });

  it("does not create a context for muted or empty notes", async () => {
    playNotes(null, [880], 0.09, 0);
    playNotes(null, [880], 0.09, -1);
    playNotes(null, [], 0.09, 0.15);
    await flush();
    expect(createContext).not.toHaveBeenCalled();
  });
});
