import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";

type Processor = {
  port: { onmessage: (e: { data: Record<string, unknown> }) => void; postMessage: ReturnType<typeof vi.fn>; close: ReturnType<typeof vi.fn> };
  process: (inputs: Float32Array[][], outputs: Float32Array[][]) => boolean;
  denoising: boolean;
  denoiser: { process: ReturnType<typeof vi.fn>; destroy: ReturnType<typeof vi.fn> };
};
const make = (size = 960) => {
  let Capture!: new (options: unknown) => Processor;
  class Base {
    port = { postMessage: vi.fn(), close: vi.fn(), onmessage: () => {} };
  }
  runInNewContext(readFileSync(new URL("../src/lib/voice/capture.worklet.js", import.meta.url), "utf8").replace(/^export /gm, ""), {
    AudioWorkletProcessor: Base,
    registerProcessor: (_name: string, ctor: typeof Capture) => (Capture = ctor),
  });
  const p = new Capture({ processorOptions: { frameSize: size } });
  const message = (data: Record<string, unknown>) => p.port.onmessage({ data });
  const process = (samples = new Float32Array(size)) => p.process([[samples]], [[new Float32Array(samples.length)]]);
  return { p, message, process };
};

describe("capture processor lifetime", () => {
  it("does no frame or denoising work before capture starts or while stopped", () => {
    const { p, message, process } = make();
    p.denoising = true;
    p.denoiser = { process: vi.fn(() => 0), destroy: vi.fn() };
    process();
    expect(p.port.postMessage).not.toHaveBeenCalled();
    expect(p.denoiser.process).not.toHaveBeenCalled();
    message({ active: true });
    process();
    expect(p.port.postMessage).toHaveBeenCalledTimes(1);
    expect(p.denoiser.process).toHaveBeenCalledTimes(2);
    message({ active: false });
    process();
    expect(p.port.postMessage).toHaveBeenCalledTimes(1);
    expect(p.denoiser.process).toHaveBeenCalledTimes(2);
    message({ active: true });
    process();
    expect(p.port.postMessage).toHaveBeenCalledTimes(2);
  });

  it("does not mix old partial frames or echo into a restarted capture", () => {
    const { p, message, process } = make(8);
    message({ active: true });
    p.process([[new Float32Array(8).fill(9)]], []); // leave queued echo
    p.port.postMessage.mockClear();
    process(new Float32Array(4).fill(1));
    message({ active: false });
    message({ active: true });
    const out = new Float32Array(4);
    p.process([[new Float32Array(4).fill(2)]], [[out]]);
    expect([...out]).toEqual([0, 0, 0, 0]);
    expect(p.port.postMessage).not.toHaveBeenCalled();
    process(new Float32Array(4).fill(3));
    expect([...p.port.postMessage.mock.calls[0]![0].samples]).toEqual([2, 2, 2, 2, 3, 3, 3, 3]);
  });

  it("terminates and frees its denoiser when closed", () => {
    const { p, message, process } = make();
    const denoiser = { process: vi.fn(), destroy: vi.fn() };
    p.denoiser = denoiser;
    message({ stop: true });
    expect(process()).toBe(false);
    expect(denoiser.destroy).toHaveBeenCalledTimes(1);
    expect(p.port.close).toHaveBeenCalledTimes(1);
    expect(p.port.postMessage).not.toHaveBeenCalled();
  });
});
