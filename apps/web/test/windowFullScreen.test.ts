// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { windowFullScreen } from "../src/lib/windowFullScreen";

const setup = () => {
  const el = document.createElement("video");
  el.showPopover = vi.fn();
  el.hidePopover = vi.fn();
  const setFullscreen = vi.fn(() => Promise.resolve());
  const enter = windowFullScreen(el, () => Promise.resolve({ setFullscreen }));
  return { el, setFullscreen, enter };
};
const flush = () => new Promise((r) => setTimeout(r));

describe("windowFullScreen", () => {
  it("makes the window full screen and the element fill it", async () => {
    const { el, setFullscreen, enter } = setup();
    enter();
    await flush();
    expect(el.classList.contains("window-fullscreen")).toBe(true);
    expect(el.showPopover).toHaveBeenCalled();
    expect(setFullscreen).toHaveBeenCalledWith(true);
  });

  it("ends on Escape, and only once", async () => {
    const { el, setFullscreen, enter } = setup();
    enter();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    el.dispatchEvent(new MouseEvent("dblclick"));
    await flush();
    expect(el.classList.contains("window-fullscreen")).toBe(false);
    expect(el.hidePopover).toHaveBeenCalledTimes(1);
    expect(setFullscreen).toHaveBeenLastCalledWith(false);
    expect(setFullscreen).toHaveBeenCalledTimes(2);
  });

  it("ends on a double click", async () => {
    const { el, setFullscreen, enter } = setup();
    enter();
    el.dispatchEvent(new MouseEvent("dblclick"));
    await flush();
    expect(el.classList.contains("window-fullscreen")).toBe(false);
    expect(setFullscreen).toHaveBeenLastCalledWith(false);
  });
});
