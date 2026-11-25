import { describe, expect, it } from "vitest";
import { initialState, type ServerState, type User } from "@kallisti/protocol";
import { mayPost, readOnlyAction } from "../src/lib/readOnly";
import { initialTextState, manageRequest, parseServerEvent, reduceText, type TextState } from "../src/lib/textChannels";

// Channel 7 (a mirror) is read-only; user 9 may post there.
const text: TextState = { ...initialTextState, readOnly: { 7: [{ id: 9, name: "news" }] }, roles: { admins: [3], moderators: [5] } };
const as = (userId: number | null): ServerState => ({
  ...initialState,
  session: 1,
  users: { 1: { session: 1, name: "me", userId } as User },
});

describe("mayPost", () => {
  it("lets everyone post in other channels", () => {
    expect(mayPost(as(null), text, 8)).toBe(true);
    expect(mayPost(as(null), text, null)).toBe(true);
  });
  it("lets only staff and the chosen users post in a read-only channel", () => {
    expect(mayPost(as(null), text, 7)).toBe(false);
    expect(mayPost(as(4), text, 7)).toBe(false);
    expect(mayPost(as(9), text, 7)).toBe(true);
    expect(mayPost(as(5), text, 7)).toBe(true);
    expect(mayPost(as(0), text, 7)).toBe(true); // SuperUser
  });
});

describe("readOnlyAction", () => {
  it("asks for a change only", () => {
    const off = { on: false, speakers: [] };
    expect(readOnlyAction(7, off, off)).toBeNull();
    expect(readOnlyAction(7, off, { on: true, speakers: [{ id: 9, name: "news" }] })).toEqual({ readOnly: 7, on: true, users: [9] });
  });
});

describe("read-only events", () => {
  it("come with ready and on their own", () => {
    const ready = parseServerEvent(JSON.stringify({ type: "ready", channels: [], readOnly: [{ channel: 7, speakers: [{ id: 9, name: "news" }] }] }));
    const state = reduceText(initialTextState, ready!);
    expect(state.readOnly).toEqual({ 7: [{ id: 9, name: "news" }] });
    expect(reduceText(state, parseServerEvent(JSON.stringify({ type: "read-only", channels: [] }))!).readOnly).toEqual({});
  });
  it("are asked for with set-read-only", () => {
    expect(JSON.parse(manageRequest({ readOnly: 7, on: true, users: [9] }))).toEqual({ type: "set-read-only", voice: 7, on: true, users: [9] });
  });
});
