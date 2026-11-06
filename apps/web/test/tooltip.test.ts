import { describe, expect, it } from "vitest";
import { tipPlace } from "../src/lib/tooltip";

describe("tooltip place", () => {
  const view = { width: 1000, height: 800 };
  it("is centred above the element, 8px away", () => {
    expect(tipPlace({ left: 100, top: 300, width: 40, height: 30 }, { width: 60, height: 24 }, view)).toEqual({ x: 90, y: 268, below: false });
  });
  it("goes below when there is no room above, and stays in the window", () => {
    expect(tipPlace({ left: 0, top: 10, width: 20, height: 20 }, { width: 80, height: 24 }, view)).toEqual({ x: 4, y: 38, below: true });
    expect(tipPlace({ left: 980, top: 300, width: 20, height: 20 }, { width: 80, height: 24 }, view).x).toBe(916);
  });
});
