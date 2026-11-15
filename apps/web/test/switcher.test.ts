import { describe, expect, it } from "vitest";
import { matchPlaces, navAction, stepPlace } from "../src/lib/switcher";

const place = (key: string, name: string) => ({ key, name });

describe("matchPlaces", () => {
  const all = [place("t1", "general"), place("t2", "game-night"), place("t3", "off-topic"), place("d1", "Gary"), place("v1", "Lounge 2")];

  it("puts names that start with the query first, then word starts, then the rest", () => {
    expect(matchPlaces(all, "g").map((p) => p.key)).toEqual(["t1", "t2", "d1", "v1"]);
    expect(matchPlaces(all, "top").map((p) => p.key)).toEqual(["t3"]);
    expect(matchPlaces(all, "lo2").map((p) => p.key)).toEqual(["v1"]); // letters in order
    expect(matchPlaces(all, "GEN").map((p) => p.key)).toEqual(["t1", "t2"]); // g…e…n in game-night
  });

  it("gives all places for no query", () => {
    expect(matchPlaces(all, " ")).toHaveLength(5);
  });
});

describe("stepPlace", () => {
  const keys = ["t1", "t2", "t3", "d1"];

  it("goes to the place above or below, around the end", () => {
    expect(stepPlace(keys, "t2", 1)).toBe("t3");
    expect(stepPlace(keys, "d1", 1)).toBe("t1");
    expect(stepPlace(keys, "t1", -1)).toBe("d1");
    expect(stepPlace(keys, null, 1)).toBe("t1");
  });

  it("can skip to the next unread place", () => {
    expect(stepPlace(keys, "t1", 1, (k) => k === "d1")).toBe("d1");
    expect(stepPlace(keys, "t1", -1, (k) => k === "t2")).toBe("t2");
    expect(stepPlace(keys, "t1", 1, () => false)).toBeNull();
  });

  it("starts at an end when the current place is not in the list", () => {
    expect(stepPlace(keys, "x", -1)).toBe("d1");
    expect(stepPlace(keys, "x", 1)).toBe("t1");
  });
});

describe("navAction", () => {
  const key = (k: Partial<KeyboardEvent>) =>
    ({ key: "", code: "", ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, defaultPrevented: false, isComposing: false, ...k }) as KeyboardEvent;

  it("knows the shortcuts, with Cmd on a Mac and Ctrl elsewhere", () => {
    expect(navAction(key({ key: "k", ctrlKey: true }), false)).toBe("switcher");
    expect(navAction(key({ key: "k", metaKey: true }), true)).toBe("switcher");
    expect(navAction(key({ key: "k", ctrlKey: true }), true)).toBeNull(); // macOS: delete to the line end
    expect(navAction(key({ key: "л", code: "KeyK", ctrlKey: true }), false)).toBe("switcher"); // other layouts
    expect(navAction(key({ key: "/", ctrlKey: true }), false)).toBe("shortcuts");
    expect(navAction(key({ key: "ArrowDown", altKey: true }), false)).toEqual({ step: 1, unread: false });
    expect(navAction(key({ key: "ArrowUp", altKey: true, shiftKey: true }), false)).toEqual({ step: -1, unread: true });
  });

  it("opens settings only with unmodified Cmd+comma on Mac", () => {
    const comma = { key: ",", metaKey: true };
    expect(navAction(key(comma), true)).toBe("settings");
    expect(navAction(key(comma), false)).toBeNull();
    for (const modifier of ["ctrlKey", "altKey", "shiftKey", "defaultPrevented", "isComposing"]) {
      expect(navAction(key({ ...comma, [modifier]: true }), true)).toBeNull();
    }
    expect(navAction(key({ key: ",", ctrlKey: true }), true)).toBeNull();
  });

  it("leaves keys alone that something else used, or an IME", () => {
    expect(navAction(key({ key: "ArrowUp", altKey: true, defaultPrevented: true }), false)).toBeNull();
    expect(navAction(key({ key: "k", ctrlKey: true, isComposing: true }), false)).toBeNull();
    expect(navAction(key({ key: "k" }), false)).toBeNull();
    expect(navAction(key({ key: "ArrowUp" }), false)).toBeNull();
  });
});
