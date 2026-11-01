import { describe, expect, it } from "vitest";
import { awayFromBottom, nearTop, newSince, nextScrollTop, olderOnly } from "../src/lib/scroll";

// A 500 px high view.
const before = { scrollHeight: 2000, scrollTop: 1500, clientHeight: 500 };

describe("nextScrollTop", () => {
  it("follows new messages when the view was at the bottom", () => {
    expect(nextScrollTop(before, 2100, false)).toBe(1600);
  });

  it("counts as at the bottom within a small margin", () => {
    expect(nextScrollTop({ ...before, scrollTop: 1450 }, 2100, false)).toBe(1600);
  });

  it("keeps the reading position when scrolled up", () => {
    // New message at the bottom: the content above does not move.
    expect(nextScrollTop({ ...before, scrollTop: 300 }, 2100, false)).toBe(300);
  });

  it("keeps the reading position when older messages load above", () => {
    expect(nextScrollTop({ ...before, scrollTop: 0 }, 2800, false, true)).toBe(800);
  });

  it("goes to the bottom on a channel switch", () => {
    expect(nextScrollTop({ ...before, scrollTop: 0 }, 900, true)).toBe(400);
  });
});

describe("nextScrollTop without a snapshot", () => {
  it("leaves the reader where they are (a re-run without new content), unless the conversation changed", () => {
    expect(nextScrollTop(null, 2654, false)).toBeNull();
    expect(nextScrollTop(null, 2654, true)).toBe(2654);
  });
});

describe("jump to present", () => {
  it("shows when more than half a screen above the bottom", () => {
    expect(awayFromBottom({ scrollHeight: 2000, scrollTop: 1500, clientHeight: 500 })).toBe(false);
    expect(awayFromBottom({ scrollHeight: 2000, scrollTop: 1300, clientHeight: 500 })).toBe(false);
    expect(awayFromBottom({ scrollHeight: 2000, scrollTop: 1200, clientHeight: 500 })).toBe(true);
  });

  it("counts messages newer than the newest one when the reader left the bottom", () => {
    expect(newSince([1, 2, 3, 4], 2)).toBe(2);
    expect(newSince([1, 2], null)).toBe(0);
  });
});

describe("older messages while scrolling up", () => {
  const at = (scrollTop: number) => ({ scrollHeight: 5000, scrollTop, clientHeight: 600 });
  it("load within one and a half screens of the top", () => {
    expect(nearTop(at(0))).toBe(true);
    expect(nearTop(at(899))).toBe(true);
    expect(nearTop(at(900))).toBe(false);
  });
  it("not in a hidden list", () => {
    expect(nearTop({ scrollHeight: 0, scrollTop: 0, clientHeight: 0 })).toBe(false);
  });
});

describe("an update that only adds older messages", () => {
  const m = (at: number) => ({ at });
  const [a, b, c] = [m(1), m(2), m(3)];
  it("is one with older messages in front of the same newest", () => {
    expect(olderOnly([b, c], [a, b, c])).toBe(true);
    expect(olderOnly([b, c], [m(1), m(2), m(3)])).toBe(true); // new objects
  });
  it("is not a new message, an edit, a first load or a switch", () => {
    expect(olderOnly([a, b], [a, b, c])).toBe(false);
    expect(olderOnly([a, b], [a, m(2)])).toBe(false);
    expect(olderOnly([], [a, b])).toBe(false);
    expect(olderOnly([b, c], [a, b, m(4)])).toBe(false);
    expect(olderOnly([a, b], [a, b])).toBe(false);
  });
});
