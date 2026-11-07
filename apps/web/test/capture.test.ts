import { afterEach, describe, expect, it, vi } from "vitest";
import { createCapture } from "../src/lib/voice/capture.svelte";
import type { EncoderSink } from "../src/lib/voice/codec";

// Encoders that only count: a test fails them through their sink.
const encoders = vi.hoisted(() => [] as { encode: ReturnType<typeof vi.fn>; flush: ReturnType<typeof vi.fn>; close: ReturnType<typeof vi.fn>; sink: EncoderSink }[]);
vi.mock("../src/lib/voice/codec", () => ({
  voiceEncoder: (_config: unknown, sink: EncoderSink) => {
    const e = { encode: vi.fn(), flush: vi.fn(), close: vi.fn(), sink };
    encoders.push(e);
    return e;
  },
}));

// Just enough of the browser's media APIs for start().
const fakeNode = () => ({ connect: vi.fn((n: unknown) => n), disconnect: vi.fn(), port: { postMessage: vi.fn() } });

const setup = () => {
  const tracks: { stop: ReturnType<typeof vi.fn>; readyState: string }[] = [];
  const getUserMedia = vi.fn(async () => {
    const track = { stop: vi.fn(), readyState: "live", label: "mic", onended: null, getSettings: () => ({ channelCount: 2 }) };
    tracks.push(track);
    return { getTracks: () => [track], getAudioTracks: () => [track] };
  });
  vi.stubGlobal("navigator", { mediaDevices: { getUserMedia } });
  vi.stubGlobal("GainNode", vi.fn(function () {
    return fakeNode();
  }));
  vi.stubGlobal("AudioWorkletNode", vi.fn(function () {
    return fakeNode();
  }));
  vi.stubGlobal("AudioEncoder", vi.fn(function () {
    return { configure: vi.fn(), close: vi.fn(), state: "configured" };
  }));
  const addModule = vi.fn(async (_url: URL) => {});
  const ctx = {
    audioWorklet: { addModule },
    destination: {},
    createMediaStreamSource: vi.fn(() => fakeNode()),
  } as unknown as AudioContext;
  const make = (mic?: Parameters<typeof createCapture>[2]) => createCapture(ctx, () => {}, mic);
  return { tracks, getUserMedia, addModule, ctx, make, capture: make() };
};

describe("capture start", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("pauses the processor when capture stops and reuses it on restart", async () => {
    const { capture } = setup();
    await capture.start();
    const node = vi.mocked(AudioWorkletNode).mock.results[0]!.value as ReturnType<typeof fakeNode>;
    expect(node.port.postMessage).toHaveBeenCalledWith({ active: true });
    capture.stop();
    expect(node.port.postMessage).toHaveBeenLastCalledWith({ active: false });
    await capture.start();
    expect(vi.mocked(AudioWorkletNode)).toHaveBeenCalledTimes(1);
    expect(node.port.postMessage).toHaveBeenLastCalledWith({ active: true });
    capture.close();
    await Promise.resolve();
    expect(node.port.postMessage).toHaveBeenLastCalledWith({ stop: true });
  });

  it("stops the microphone when the worklet fails to load", async () => {
    const { tracks, addModule, capture } = setup();
    addModule.mockRejectedValueOnce(new Error("no worklet"));
    await capture.start();
    expect(capture.micError).toBe("no worklet");
    expect(tracks.map((t) => t.stop.mock.calls.length)).toEqual([1]);
  });

  it("loads the worklet again after a failure", async () => {
    const { tracks, addModule, capture } = setup();
    addModule.mockRejectedValueOnce(new Error("no worklet"));
    await capture.start();
    await capture.start();
    expect(addModule).toHaveBeenCalledTimes(2);
    expect(capture.micError).toBeNull();
    expect(tracks.map((t) => t.stop.mock.calls.length)).toEqual([1, 0]); // the second one is in use
  });

  it("opens the microphone again without filters for music mode", async () => {
    const { getUserMedia, capture } = setup();
    await capture.start("mic-2");
    capture.setMusicMode(true);
    await vi.waitFor(() => expect(getUserMedia).toHaveBeenCalledTimes(2));
    expect(getUserMedia.mock.lastCall).toEqual([
      { audio: { deviceId: { exact: "mic-2" }, echoCancellation: false, noiseSuppression: false, autoGainControl: false, channelCount: { ideal: 2 } } },
    ]);
    // The microphone has two channels: the worklet collects both.
    const node = vi.mocked(AudioWorkletNode).mock.results[0]!.value as ReturnType<typeof fakeNode>;
    await vi.waitFor(() => expect(node.port.postMessage).toHaveBeenCalledWith({ channels: 2 }));
  });

  it("opens the microphone again when music mode changes while it opens", async () => {
    const { getUserMedia, tracks, capture } = setup();
    const opening = capture.start();
    capture.setMusicMode(true); // during the permission prompt
    await opening;
    await vi.waitFor(() => expect(getUserMedia).toHaveBeenCalledTimes(2));
    expect(getUserMedia.mock.lastCall).toMatchObject([{ audio: { autoGainControl: false } }]);
    await vi.waitFor(() => expect(tracks.map((t) => t.stop.mock.calls.length)).toEqual([1, 0]));
  });
});

describe("capture with a failed encoder", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("stops feeding it and tries a fresh one at the next talk spurt", async () => {
    encoders.length = 0;
    const { capture } = setup();
    capture.setMode("ptt");
    capture.setPttDown(true);
    await capture.start();
    const node = vi.mocked(AudioWorkletNode).mock.results[0]!.value as ReturnType<typeof fakeNode> & { port: { onmessage: (e: unknown) => void } };
    const frame = () => node.port.onmessage({ data: { samples: new Float32Array(960) } });
    frame();
    expect(encoders[0]!.encode).toHaveBeenCalledTimes(1);
    encoders[0]!.sink.error(new Error("no wasm"));
    expect(capture.micError).toBe("Voice encoder failed: no wasm");
    frame();
    capture.setPttDown(false);
    frame();
    expect(encoders[0]!.encode).toHaveBeenCalledTimes(1);
    expect(encoders).toHaveLength(1);
    capture.setPttDown(true);
    frame();
    expect(encoders[1]!.encode).toHaveBeenCalledTimes(1);
  });
});

// A restart for a new output device: the new context's capture takes the
// open microphone over (no new permission prompt, no gap in the mic).
describe("capture handover", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("keeps the microphone open and the next capture uses it without asking again", async () => {
    const { tracks, getUserMedia, ctx, capture, make } = setup();
    await capture.start("mic-2");
    const stream = await getUserMedia.mock.results[0]!.value;
    const mic = capture.close(true);
    expect(tracks[0]!.stop).not.toHaveBeenCalled();
    const next = make(mic);
    await next.start("mic-2");
    expect(getUserMedia).toHaveBeenCalledTimes(1);
    expect(vi.mocked(ctx.createMediaStreamSource).mock.lastCall).toEqual([stream]);
    expect(next.deviceLabel).toBe("mic");
    expect(tracks[0]!.stop).not.toHaveBeenCalled();
  });

  it("opens the microphone again for another device or other filters, and stops the old one", async () => {
    const { tracks, getUserMedia, capture, make } = setup();
    await capture.start("mic-2");
    const next = make(capture.close(true));
    next.setEchoCancellation(false);
    await next.start("mic-2");
    expect(getUserMedia).toHaveBeenCalledTimes(2);
    expect(tracks[0]!.stop).toHaveBeenCalled();
  });

  it("does not use a microphone that ended meanwhile", async () => {
    const { tracks, getUserMedia, capture, make } = setup();
    await capture.start();
    const next = make(capture.close(true));
    tracks[0]!.readyState = "ended";
    await next.start();
    expect(getUserMedia).toHaveBeenCalledTimes(2);
  });

  it("stops a microphone it was handed but did not use", async () => {
    const { tracks, capture, make } = setup();
    await capture.start();
    const next = make(capture.close(true));
    next.stop();
    expect(tracks[0]!.stop).toHaveBeenCalled();
  });

  it("passes an unused microphone on (two restarts in a row)", async () => {
    const { tracks, getUserMedia, capture, make } = setup();
    await capture.start();
    const second = make(capture.close(true));
    const third = make(second.close(true));
    await third.start();
    expect(getUserMedia).toHaveBeenCalledTimes(1);
    expect(tracks[0]!.stop).not.toHaveBeenCalled();
  });

  it("a plain close stops the microphone and hands nothing over", async () => {
    const { tracks, capture } = setup();
    await capture.start();
    expect(capture.close()).toBeNull();
    expect(tracks[0]!.stop).toHaveBeenCalled();
  });
});
