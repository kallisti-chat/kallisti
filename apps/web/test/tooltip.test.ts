// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { tip, tipPlace } from "../src/lib/tooltip";

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

describe("tooltip with a delay (a reaction's card)", () => {
  const button = () => {
    const b = document.createElement("button");
    b.innerHTML = '<span class="emoji">👀</span>3';
    document.body.append(b);
    return b;
  };
  const hover = (b: HTMLElement, type: string, pointerType = "mouse") =>
    b.dispatchEvent(Object.assign(new Event(type), { pointerType }));
  const shown = () => document.querySelector(".tooltip");

  it("shows after the delay, with a large copy of the emoji", () => {
    vi.useFakeTimers();
    const b = button();
    const off = tip("tam and you reacted", { delay: 500, emoji: true })(b);
    hover(b, "pointerenter");
    vi.advanceTimersByTime(499);
    expect(shown()).toBeNull();
    vi.advanceTimersByTime(1);
    expect(shown()?.querySelector(".tip-emoji")?.textContent).toBe("👀");
    expect(shown()?.textContent).toContain("tam and you reacted");
    hover(b, "pointerleave");
    expect(shown()).toBeNull();
    off();
    vi.useRealTimers();
  });
  it("does not show when the pointer leaves first, or for a touch", () => {
    vi.useFakeTimers();
    const b = button();
    const off = tip("x", { delay: 500 })(b);
    hover(b, "pointerenter");
    vi.advanceTimersByTime(200);
    hover(b, "pointerleave");
    hover(b, "pointerenter", "touch");
    vi.advanceTimersByTime(1000);
    expect(shown()).toBeNull();
    off();
    vi.useRealTimers();
  });
});
