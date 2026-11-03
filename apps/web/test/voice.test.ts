import { describe, expect, it } from "vitest";
import { loadSelfState, micWanted, saveSelfState, soundHint, voiceJoined } from "../src/lib/voice";

describe("micWanted", () => {
  const s = { inVoice: true, selfMute: false, settingsOpen: false, closeWhenMuted: false };
  it("opens the microphone in voice (also muted: no dropout on unmute), or for the settings", () => {
    expect(micWanted(s)).toBe(true);
    expect(micWanted({ ...s, selfMute: true })).toBe(true);
    expect(micWanted({ ...s, inVoice: false })).toBe(false);
    // The settings show the level and test the threshold.
    expect(micWanted({ ...s, inVoice: false, selfMute: true, settingsOpen: true })).toBe(true);
  });

  it("closes it when muted if the user wants that", () => {
    expect(micWanted({ ...s, closeWhenMuted: true })).toBe(true);
    expect(micWanted({ ...s, closeWhenMuted: true, selfMute: true })).toBe(false);
    expect(micWanted({ ...s, closeWhenMuted: true, selfMute: true, settingsOpen: true })).toBe(true);
  });
});

describe("saved self state", () => {
  const memory = () => {
    const items = new Map<string, string>();
    return { getItem: (k: string) => items.get(k) ?? null, setItem: (k: string, v: string) => void items.set(k, v) };
  };

  it("keeps mute and deafen per server", () => {
    const storage = memory();
    expect(loadSelfState(storage, "https://a.example")).toEqual({ selfMute: false, selfDeaf: false });
    saveSelfState(storage, "https://a.example", { selfMute: true, selfDeaf: false });
    expect(loadSelfState(storage, "https://a.example")).toEqual({ selfMute: true, selfDeaf: false });
    expect(loadSelfState(storage, "https://b.example")).toEqual({ selfMute: false, selfDeaf: false });
    saveSelfState(storage, "https://a.example", { selfMute: true, selfDeaf: true });
    expect(loadSelfState(storage, "https://a.example")).toEqual({ selfMute: true, selfDeaf: true });
  });

  it("reads deaf as also muted, and anything odd as neither", () => {
    const storage = memory();
    storage.setItem("selfState:https://a.example", "deaf");
    expect(loadSelfState(storage, "https://a.example")).toEqual({ selfMute: true, selfDeaf: true });
    storage.setItem("selfState:https://a.example", "{garbage");
    expect(loadSelfState(storage, "https://a.example")).toEqual({ selfMute: false, selfDeaf: false });
    expect(loadSelfState(undefined, "https://a.example")).toEqual({ selfMute: false, selfDeaf: false });
  });
});

describe("soundHint", () => {
  it("asks for a click only in voice while the browser holds the sound back", () => {
    expect(soundHint("suspended", true)).toBe(true);
    expect(soundHint("interrupted", true)).toBe(true);
    expect(soundHint("interrupted", false)).toBe(false);
    expect(soundHint("suspended", false)).toBe(false);
    expect(soundHint("running", true)).toBe(false);
    expect(soundHint("closed", true)).toBe(false);
    expect(soundHint(null, true)).toBe(false);
  });
});

describe("one voice channel across servers", () => {
  it("the server that joined last stays (the others leave once)", () => {
    expect(voiceJoined([], ["home"])).toBe("home");
    expect(voiceJoined(["home"], ["home"])).toBeNull();
    expect(voiceJoined(["home"], ["home", "b"])).toBe("b");
    // Until the server answers, both are in voice: no second leave, no flip.
    expect(voiceJoined(["home", "b"], ["home", "b"])).toBeNull();
    expect(voiceJoined(["home", "b"], ["b"])).toBeNull();
    // Two at once (a reload restoring both): the last one stays.
    expect(voiceJoined([], ["home", "b"])).toBe("b");
  });
});
