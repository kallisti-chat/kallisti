import { describe, expect, it } from "vitest";
import { encodeFrame, isCamera, ownerOf, streamId } from "../src/lib/stream/protocol";
import {
  DEFAULT_LIMITS,
  DEFAULT_QUALITY,
  cameraConstraints,
  encoding,
  nextCamera,
  parseLimits,
  qualityLabel,
  qualityOptions,
  sources,
  streamLabel,
} from "../src/lib/stream/quality";

describe("stream ids", () => {
  it("are the session for a screen share, with the top bit for a camera", () => {
    expect(streamId(5, false)).toBe(5);
    expect(streamId(5, true)).toBe(2 ** 31 + 5);
    expect(ownerOf(2 ** 31 + 5)).toBe(5);
    expect(ownerOf(5)).toBe(5);
    expect(isCamera(2 ** 31 + 5)).toBe(true);
    expect(isCamera(5)).toBe(false);
  });
  it("mark camera frames", () => {
    expect(encodeFrame(true, 0, new Uint8Array(), false, true)[0]).toBe(5);
  });
});

describe("encoding", () => {
  const limits = DEFAULT_LIMITS;
  it("fits the chosen height, keeps the aspect and the frame rate", () => {
    expect(encoding({ width: 3840, height: 2160 }, { height: 720, fps: 30 }, "screen", limits)).toMatchObject({ width: 1280, height: 720, framerate: 30 });
    expect(encoding({ width: 640, height: 480 }, { height: 1080, fps: 15 }, "camera", limits)).toMatchObject({ width: 640, height: 480 });
  });
  it("takes the height as the short side of a portrait picture (phones)", () => {
    expect(encoding({ width: 1080, height: 1920 }, { height: 720, fps: 30 }, "camera", limits)).toMatchObject({ width: 720, height: 1280 });
  });
  it("keeps within the server's largest side", () => {
    expect(encoding({ width: 3840, height: 2160 }, { height: 1080, fps: 30 }, "screen", { ...limits, maxSide: 1280 })).toMatchObject({ width: 1280, height: 720 });
  });
  it("grows the bitrate with size and frame rate, within the server's upload limit", () => {
    const low = encoding({ width: 1920, height: 1080 }, { height: 480, fps: 15 }, "screen", limits).bitrate;
    const high = encoding({ width: 1920, height: 1080 }, { height: 1080, fps: 60 }, "screen", limits).bitrate;
    expect(low).toBeLessThan(high);
    // Room is left for sound and keyframes.
    expect(high).toBeLessThanOrEqual(limits.bytesPerSecond * 8 * 0.8);
    expect(encoding({ width: 1920, height: 1080 }, { height: 1080, fps: 60 }, "screen", { ...limits, bytesPerSecond: 1000 }).bitrate).toBeGreaterThanOrEqual(100_000);
  });
  it("is about 4 Mbit/s at 720p and 30 fps", () => {
    const { bitrate } = encoding({ width: 1920, height: 1080 }, { height: 720, fps: 30 }, "screen", limits);
    expect(bitrate).toBeGreaterThan(3_800_000);
    expect(bitrate).toBeLessThan(4_200_000);
  });
  it("shares the upload when screen and camera run together, the screen getting more", () => {
    const q = { height: 1080, fps: 60 };
    const screen = encoding({ width: 1920, height: 1080 }, q, "screen", limits, true).bitrate;
    const camera = encoding({ width: 1920, height: 1080 }, q, "camera", limits, true).bitrate;
    expect(screen + camera).toBeLessThanOrEqual(limits.bytesPerSecond * 8 * 0.8);
    expect(screen).toBeGreaterThan(camera);
    expect(screen).toBeLessThan(encoding({ width: 1920, height: 1080 }, q, "screen", limits).bitrate);
  });
});

describe("qualityOptions", () => {
  it("are the heights within the server's largest side, and three frame rates", () => {
    expect(qualityOptions(DEFAULT_LIMITS)).toEqual({ heights: [480, 720, 1080], fps: [15, 30, 60] });
    expect(qualityOptions({ ...DEFAULT_LIMITS, maxSide: 800 }).heights).toEqual([480, 720]);
    expect(qualityOptions({ ...DEFAULT_LIMITS, maxFps: 30 }).fps).toEqual([15, 30]);
  });
});

describe("parseLimits", () => {
  it("takes the server's limits, defaults otherwise", () => {
    expect(parseLimits({ bytesPerSecond: 500_000, maxSide: 2000 })).toEqual({ bytesPerSecond: 500_000, maxSide: 2000, maxFps: 60 });
    expect(parseLimits({ bytesPerSecond: 500_000, maxSide: 2000, maxFps: 30 })).toEqual({ bytesPerSecond: 500_000, maxSide: 2000, maxFps: 30 });
    expect(parseLimits(undefined)).toEqual(DEFAULT_LIMITS);
    expect(parseLimits({ bytesPerSecond: "x", maxSide: -1 })).toEqual(DEFAULT_LIMITS);
  });
});

describe("labels", () => {
  it("name a quality and a stream", () => {
    expect(qualityLabel({ height: 720, fps: 30 })).toBe("720p 30 fps");
    expect(streamLabel("Ann", false)).toBe("Ann's screen");
    expect(streamLabel("Ann", true)).toBe("Ann's camera");
  });
  it("defaults to 720p at 30 fps", () => {
    expect(DEFAULT_QUALITY.screen).toEqual({ height: 720, fps: 30 });
    expect(DEFAULT_QUALITY.camera).toEqual({ height: 720, fps: 30 });
  });
});

describe("sources", () => {
  const all = { encode: true, display: true, camera: true, phone: false };
  it("offers what the browser can capture and encode", () => {
    expect(sources(all)).toEqual({ screen: true, camera: true });
    expect(sources({ ...all, encode: false })).toEqual({ screen: false, camera: false });
    expect(sources({ ...all, display: false })).toEqual({ screen: false, camera: true });
  });
  it("offers phones only the camera (they cannot share the screen)", () => {
    expect(sources({ ...all, phone: true })).toEqual({ screen: false, camera: true });
  });
});

describe("cameraConstraints", () => {
  it("ask for the chosen camera at the chosen quality", () => {
    expect(cameraConstraints("cam1", { height: 720, fps: 30 })).toEqual({
      deviceId: { exact: "cam1" },
      width: { ideal: 1280 },
      height: { ideal: 720 },
      frameRate: { ideal: 30 },
    });
    expect(cameraConstraints(null, { height: 480, fps: 15 }).deviceId).toBeUndefined();
  });
});

describe("nextCamera", () => {
  const cams = [{ deviceId: "a" }, { deviceId: "b" }, { deviceId: "c" }];
  it("is the next camera in the list, round", () => {
    expect(nextCamera(cams, "a")).toBe("b");
    expect(nextCamera(cams, "c")).toBe("a");
  });
  it("is the second one when the current is unknown (the first is the default)", () => {
    expect(nextCamera(cams, null)).toBe("b");
  });
  it("is null with fewer than two cameras", () => {
    expect(nextCamera([{ deviceId: "a" }], "a")).toBeNull();
  });
});
