import { describe, expect, it } from "vitest";
import { initialState, Perm } from "@kallisti/protocol";
import { mayChangeGuests } from "../src/lib/guests";
import { confirmsManage } from "../src/lib/saveFeedback";
import { initialTextState, manageRequest, parseServerEvent, reduceText } from "../src/lib/textChannels";

describe("random image names", () => {
  it("defaults off and preserves explicit booleans from ready", () => {
    expect(initialTextState.randomImageNames).toBe(false);
    for (const value of [true, false]) {
      const event = parseServerEvent(JSON.stringify({ type: "ready", channels: [], randomImageNames: value }))!;
      expect(event).toHaveProperty("randomImageNames", value);
      expect(reduceText(initialTextState, event).randomImageNames).toBe(value);
    }
    for (const value of [undefined, "true", 1, null]) {
      const event = parseServerEvent(JSON.stringify({ type: "ready", channels: [], randomImageNames: value }))!;
      expect(reduceText({ ...initialTextState, randomImageNames: true }, event).randomImageNames).toBe(false);
    }
  });

  it("accepts setting broadcasts and rejects missing or non-boolean values", () => {
    for (const value of [true, false]) {
      const event = parseServerEvent(JSON.stringify({ type: "random-image-names", randomImageNames: value }))!;
      expect(reduceText(initialTextState, event).randomImageNames).toBe(value);
    }
    for (const value of [undefined, "false", 0, null]) {
      expect(parseServerEvent(JSON.stringify({ type: "random-image-names", randomImageNames: value }))).toBeNull();
    }
  });

  it("sends either choice and waits for the matching server acknowledgement", () => {
    for (const value of [true, false]) {
      expect(JSON.parse(manageRequest({ randomImageNames: value })!)).toEqual({ type: "set-random-image-names", randomImageNames: value });
      const confirm = confirmsManage({ randomImageNames: value })!;
      expect(confirm({ type: "random-image-names", randomImageNames: value })).toBe(true);
      expect(confirm({ type: "random-image-names", randomImageNames: !value })).toBe(false);
      expect(confirm({ type: "guests-read-only", on: value })).toBe(false);
    }
  });

  it("offers the setting only to admins of the built-in server", () => {
    const server = { ...initialState, rootPermissions: Perm.Write };
    const text = { ...initialTextState, voiceChats: true };
    expect(mayChangeGuests(server, text)).toBe(true);
    expect(mayChangeGuests({ ...server, rootPermissions: 0 }, text)).toBe(false);
    expect(mayChangeGuests(server, { ...text, voiceChats: false })).toBe(false);
  });
});
