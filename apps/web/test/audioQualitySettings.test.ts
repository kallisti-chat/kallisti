import { describe, expect, it } from "vitest";
import { toBufferMs, bufferLabel, MIN_BUFFER_MS, MAX_BUFFER_MS } from "../src/lib/voice/buffer";
import { parseSettings, DEFAULT_SETTINGS, micConstraints } from "../src/lib/settings";
import { sendChannels } from "../src/lib/voice/opus";
import { DEFAULT_QUALITY_LIMITS } from "../src/lib/textChannels";
import { qualityOptions } from "../src/lib/stream/quality";

describe("audio buffer", () => {
  it("converts preset strings to milliseconds", () => {
    expect(toBufferMs("low")).toBe(60);
    expect(toBufferMs("medium")).toBe(120);
    expect(toBufferMs("high")).toBe(250);
  });

  it("handles numeric ms values and clamps them", () => {
    expect(toBufferMs(80)).toBe(80);
    expect(toBufferMs(10)).toBe(MIN_BUFFER_MS);
    expect(toBufferMs(1000)).toBe(MAX_BUFFER_MS);
  });

  it("generates readable labels for presets and numbers", () => {
    expect(bufferLabel("low")).toContain("60 ms");
    expect(bufferLabel("medium")).toContain("120 ms");
    expect(bufferLabel("high")).toContain("250 ms");
    expect(bufferLabel(150)).toBe("150 ms");
  });
});

describe("audio channels and opus encoding", () => {
  it("selects channel count based on mode and music flag", () => {
    expect(sendChannels(false, 2, "auto")).toBe(1);
    expect(sendChannels(true, 2, "auto")).toBe(2);
    expect(sendChannels(false, 2, "mono")).toBe(1);
    expect(sendChannels(true, 2, "mono")).toBe(1);
    expect(sendChannels(false, 2, "stereo")).toBe(2);
    expect(sendChannels(false, 1, "stereo")).toBe(1); // capped by hardware channels
  });
});

describe("micConstraints with quality settings", () => {
  it("honors autoGainControl setting in speech mode", () => {
    const withAgc = micConstraints("mic-1", { browserNoise: false, cancelEcho: true, autoGainControl: true, music: false });
    expect(withAgc.autoGainControl).toBe(true);

    const withoutAgc = micConstraints("mic-1", { browserNoise: false, cancelEcho: true, autoGainControl: false, music: false });
    expect(withoutAgc.autoGainControl).toBe(false);
  });

  it("requests stereo channels when audioChannels is stereo", () => {
    const stereo = micConstraints("mic-1", { browserNoise: false, cancelEcho: false, audioChannels: "stereo", music: false });
    expect(stereo.channelCount).toEqual({ ideal: 2 });
  });

  it("allows completely disabling all filters in speech mode", () => {
    const allOff = micConstraints("mic-1", { browserNoise: false, cancelEcho: false, autoGainControl: false, music: false });
    expect(allOff.echoCancellation).toBe(false);
    expect(allOff.noiseSuppression).toBe(false);
    expect(allOff.autoGainControl).toBe(false);
  });
});

describe("settings parsing for quality", () => {
  it("uses default voice and music bitrates", () => {
    expect(DEFAULT_SETTINGS.voiceBitrate).toBe(40_000);
    expect(DEFAULT_SETTINGS.musicBitrate).toBe(128_000);
    expect(DEFAULT_SETTINGS.audioChannels).toBe("auto");
    expect(DEFAULT_SETTINGS.autoGainControl).toBe(true);
  });

  it("clamps custom bitrates and buffer numbers", () => {
    const parsed = parseSettings(JSON.stringify({
      voiceBitrate: 24_000,
      musicBitrate: 1_000_000, // above 558000
      playbackBuffer: 15, // below 30
    }));
    expect(parsed.voiceBitrate).toBe(24_000);
    expect(parsed.musicBitrate).toBe(558_000);
    expect(parsed.playbackBuffer).toBe(30);
  });
});

describe("stream quality limits", () => {
  it("caps frame rate options based on maxFps", () => {
    expect(qualityOptions({ bytesPerSecond: 1_000_000, maxSide: 1920, maxFps: 30 }).fps).toEqual([15, 30]);
    expect(qualityOptions({ bytesPerSecond: 1_000_000, maxSide: 1920, maxFps: 15 }).fps).toEqual([15]);
    expect(qualityOptions({ bytesPerSecond: 1_000_000, maxSide: 1920, maxFps: 60 }).fps).toEqual([15, 30, 60]);
  });

  it("has sensible default quality limits", () => {
    expect(DEFAULT_QUALITY_LIMITS.maxBitrateVoice).toBe(128_000);
    expect(DEFAULT_QUALITY_LIMITS.maxBitrateMusic).toBe(320_000);
    expect(DEFAULT_QUALITY_LIMITS.maxFps).toBe(60);
  });
});
