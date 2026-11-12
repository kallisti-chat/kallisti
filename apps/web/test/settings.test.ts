import { describe, expect, it } from "vitest";
import { DEFAULT_SETTINGS, keyLabel, meterPercent, parseSettings, pttAction, pttBlocksDefault, themeOf, micConstraints } from "../src/lib/settings";

describe("parseSettings", () => {
  it("uses defaults for missing or broken input", () => {
    expect(parseSettings(null)).toEqual(DEFAULT_SETTINGS);
    expect(parseSettings("not json")).toEqual(DEFAULT_SETTINGS);
    expect(parseSettings("[1,2]")).toEqual(DEFAULT_SETTINGS);
  });

  it("keeps valid values", () => {
    const stored = {
      mode: "ptt",
      pttKey: "KeyV",
      vadThresholdDb: -30,
      inputDeviceId: "mic-2",
      outputDeviceId: "speakers-1",
      notifications: true,
      closeMicWhenMuted: true,
      noiseSuppression: "strong",
      theme: "system",
      timeFormat: "24",
      echoCancellation: false,
      musicMode: true,
      playbackBuffer: "high",
      cameraDeviceId: "cam-1",
      streamQuality: { screen: { height: 720, fps: 60 }, camera: { height: 480, fps: 15 } },
      sounds: false,
      soundVolume: 0.3,
      betterTranslation: true,
    };
    expect(parseSettings(JSON.stringify(stored))).toEqual(stored);
  });

  it("replaces invalid values one by one", () => {
    const parsed = parseSettings(
      JSON.stringify({ mode: "shout", pttKey: 5, vadThresholdDb: -300, inputDeviceId: "mic-2", notifications: "yes", closeMicWhenMuted: 1, betterTranslation: "on", noiseSuppression: "max", musicMode: "on", playbackBuffer: "huge", cameraDeviceId: 3, streamQuality: { screen: { height: 333, fps: 30 }, camera: "hd" } }),
    );
    expect(parsed).toEqual({ ...DEFAULT_SETTINGS, inputDeviceId: "mic-2" });
  });

  it("clamps the threshold to the slider range", () => {
    expect(parseSettings(JSON.stringify({ vadThresholdDb: 10 })).vadThresholdDb).toBe(0);
  });
});

describe("pttAction", () => {
  const key = "Backquote";
  const ev = (code: string, extra: { repeat?: boolean; editable?: boolean } = {}) => ({
    code,
    repeat: extra.repeat ?? false,
    editable: extra.editable ?? false,
  });

  it("presses and releases on the configured key", () => {
    expect(pttAction("keydown", ev(key), key)).toBe("down");
    expect(pttAction("keyup", ev(key), key)).toBe("up");
  });

  it("ignores other keys and key repeat", () => {
    expect(pttAction("keydown", ev("KeyA"), key)).toBeNull();
    expect(pttAction("keydown", ev(key, { repeat: true }), key)).toBeNull();
  });

  it("does not press while typing, but always releases", () => {
    expect(pttAction("keydown", ev(key, { editable: true }), key)).toBeNull();
    expect(pttAction("keyup", ev(key, { editable: true }), key)).toBe("up");
  });
});

describe("keyLabel", () => {
  it("makes key codes readable", () => {
    expect(keyLabel("KeyV")).toBe("V");
    expect(keyLabel("Digit4")).toBe("4");
    expect(keyLabel("Backquote")).toBe("`");
    expect(keyLabel("ShiftLeft")).toBe("Left Shift");
    expect(keyLabel("F13")).toBe("F13");
  });
});

describe("meterPercent", () => {
  it("maps -80..0 dB to 0..100", () => {
    expect(meterPercent(-80)).toBe(0);
    expect(meterPercent(-40)).toBe(50);
    expect(meterPercent(0)).toBe(100);
    expect(meterPercent(-100)).toBe(0);
    expect(meterPercent(6)).toBe(100);
  });
});

describe("pttBlocksDefault", () => {
  const ev = (code: string, editable = false) => ({ code, repeat: false, editable });

  it("blocks the PTT key outside text fields, also on keyup and repeat", () => {
    expect(pttBlocksDefault(ev("Space"), "Space")).toBe(true);
    expect(pttBlocksDefault({ ...ev("Space"), repeat: true }, "Space")).toBe(true);
  });

  it("never blocks typing or other keys", () => {
    expect(pttBlocksDefault(ev("Space", true), "Space")).toBe(false);
    expect(pttBlocksDefault(ev("Enter"), "Space")).toBe(false);
  });
});

describe("echo cancellation", () => {
  it("is on unless turned off", () => {
    expect(DEFAULT_SETTINGS.echoCancellation).toBe(true);
    expect(parseSettings('{"echoCancellation":false}').echoCancellation).toBe(false);
    expect(parseSettings('{"echoCancellation":"no"}').echoCancellation).toBe(true);
  });
});

describe("sounds", () => {
  it("are on by default, with a volume from 0 to 1", () => {
    expect(DEFAULT_SETTINGS.sounds).toBe(true);
    expect(parseSettings('{"sounds":false}').sounds).toBe(false);
    expect(parseSettings('{"sounds":"no"}').sounds).toBe(true);
    expect(parseSettings('{"soundVolume":0}').soundVolume).toBe(0);
    expect(parseSettings('{"soundVolume":3}').soundVolume).toBe(1);
    expect(parseSettings('{"soundVolume":"loud"}').soundVolume).toBe(DEFAULT_SETTINGS.soundVolume);
  });
});

describe("time format", () => {
  it("is automatic unless set to 12 or 24 hours", () => {
    expect(DEFAULT_SETTINGS.timeFormat).toBe("auto");
    expect(parseSettings('{"timeFormat":"24"}').timeFormat).toBe("24");
    expect(parseSettings('{"timeFormat":"12"}').timeFormat).toBe("12");
    expect(parseSettings('{"timeFormat":"36"}').timeFormat).toBe("auto");
  });
});

describe("theme", () => {
  it("is dark unless chosen, light or as the system", () => {
    expect(DEFAULT_SETTINGS.theme).toBe("dark");
    expect(parseSettings('{"theme":"light"}').theme).toBe("light");
    expect(parseSettings('{"theme":"system"}').theme).toBe("system");
    expect(parseSettings('{"theme":"pink"}').theme).toBe("dark");
    expect(themeOf("system", true)).toBe("dark");
    expect(themeOf("system", false)).toBe("light");
    expect(themeOf("light", true)).toBe("light");
  });
});

describe("micConstraints", () => {
  it("asks for exactly the chosen microphone, with the filters as set", () => {
    expect(micConstraints("mic-2", { browserNoise: false, cancelEcho: false, music: false })).toEqual({
      deviceId: { exact: "mic-2" },
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: true,
    });
    expect(micConstraints(null, { browserNoise: true, cancelEcho: true, music: false })).toMatchObject({ deviceId: undefined, echoCancellation: true, noiseSuppression: true });
  });

  it("turns off all of the browser's filters in music mode, and asks for stereo", () => {
    expect(micConstraints("mic-2", { browserNoise: true, cancelEcho: true, music: true })).toEqual({
      deviceId: { exact: "mic-2" },
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: false,
      channelCount: { ideal: 2 },
    });
  });
});
