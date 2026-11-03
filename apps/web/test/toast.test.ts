import { describe, expect, it } from "vitest";
import { toastMs } from "../src/lib/toast";

describe("toastMs", () => {
  it("gives short texts 5 s and long ones time to read", () => {
    expect(toastMs("Copied")).toBe(5000);
    expect(toastMs(Array(45).fill("word").join(" "))).toBe(13500);
  });
});
