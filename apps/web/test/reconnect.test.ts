import { describe, expect, it } from "vitest";
import { proto } from "@kallisti/protocol";
import { busyName, loggedOut, refusedForGood, readyToShow, reconnectDelay, rejoinTarget, shouldReconnect, lossNotice, nextDelay, QUIET_MS } from "../src/lib/reconnect";
import type { Place } from "../src/lib/place";

const lost = { status: "synced" as const, closed: true, closeReason: "", wasSynced: true };

describe("shouldReconnect", () => {
  it("comes back after an unexpected close", () => {
    expect(shouldReconnect(lost)).toBe(true);
    expect(shouldReconnect({ ...lost, closeReason: "server shutting down" })).toBe(true); // a restart
  });

  it("not while open, before it was ever in, or after a refusal", () => {
    expect(shouldReconnect({ ...lost, closed: false })).toBe(false);
    expect(shouldReconnect({ ...lost, wasSynced: false })).toBe(false);
    expect(shouldReconnect({ ...lost, status: "kicked" })).toBe(false);
    expect(shouldReconnect({ ...lost, status: "rejected" })).toBe(false);
    expect(shouldReconnect({ ...lost, closeReason: "certificate-changed ab" })).toBe(false);
  });

  it("once straight to the server when the apps' bridge could not reach it", () => {
    const failed = { ...lost, wasSynced: false, closeReason: "cannot reach wss://chat.example/ws: invalid certificate" };
    expect(shouldReconnect({ ...failed, straightNext: true })).toBe(true);
    expect(shouldReconnect(failed)).toBe(false); // a Mumble server through the bridge: the user decides
  });

  it("not after the server ended the session (logout elsewhere)", () => {
    expect(shouldReconnect({ ...lost, closeReason: "logged out" })).toBe(false);
  });
});

describe("loggedOut", () => {
  it("is the server's close after a logout or password change", () => {
    expect(loggedOut("logged out")).toBe(true);
    expect(loggedOut("session expired")).toBe(true); // a token the server does not know
    expect(loggedOut("no authentication")).toBe(false);
    expect(loggedOut("")).toBe(false);
  });
});

describe("reconnectDelay", () => {
  it("waits longer each time, up to 30 seconds", () => {
    expect([0, 1, 2, 3, 4, 5, 9].map(reconnectDelay)).toEqual([1000, 2000, 5000, 10000, 20000, 30000, 30000]);
  });
});

describe("busyName", () => {
  it("is a refusal because the old session still holds the name", () => {
    expect(busyName({ status: "rejected", rejectType: proto.Reject_RejectType.UsernameInUse })).toBe(true);
    expect(busyName({ status: "rejected", rejectType: proto.Reject_RejectType.WrongUserPW })).toBe(false);
    expect(busyName({ status: "synced", rejectType: null })).toBe(false);
  });
});

describe("rejoinTarget", () => {
  const back: Place = { session: 3, view: { kind: "voice" }, voice: 5, thread: null, selfMute: true, selfDeaf: false };
  const exists = (id: number) => id !== 9;

  it("the voice channel the user was in", () => {
    expect(rejoinTarget(back, 0, exists, null)).toBe(5);
  });

  it("none when not in voice before, already there, or the channel is gone", () => {
    expect(rejoinTarget(null, 0, exists, null)).toBe(null);
    expect(rejoinTarget({ ...back, voice: null }, 0, exists, null)).toBe(null);
    expect(rejoinTarget(back, 5, exists, null)).toBe(null);
    expect(rejoinTarget({ ...back, voice: 9 }, 0, exists, null)).toBe(null);
  });

  it("takes voice back from our old session, which the server has not let go yet", () => {
    expect(rejoinTarget(back, 0, exists, 3)).toBe(5);
  });

  it("not from another device of the account that went into voice meanwhile", () => {
    expect(rejoinTarget(back, 0, exists, 4)).toBe(null);
    expect(rejoinTarget({ ...back, session: null }, 0, exists, 4)).toBe(null);
  });
});

describe("a quiet loss", () => {
  it("shows nothing at first, a small sign after 2 s, the banner after 10 s", () => {
    expect(lossNotice(0)).toBe("none");
    expect(lossNotice(1999)).toBe("none");
    expect(lossNotice(2000)).toBe("sign");
    expect(lossNotice(9999)).toBe("sign");
    expect(lossNotice(QUIET_MS)).toBe("banner");
  });
  it("tries each second while quiet, then the growing waits from the first", () => {
    expect(nextDelay(0, 0)).toBe(1000);
    expect(nextDelay(9000, 0)).toBe(1000);
    expect(nextDelay(QUIET_MS, 0)).toBe(reconnectDelay(0));
    expect(nextDelay(QUIET_MS + 5000, 3)).toBe(reconnectDelay(3));
  });
});

describe("a refusal by the server's rules", () => {
  it("ends the reconnect: retrying cannot help", () => {
    const base = { status: "connecting" as const, closed: true, wasSynced: true };
    expect(shouldReconnect({ ...base, closeReason: "join as a guest on the server's own site" })).toBe(false);
    expect(refusedForGood("join as a guest on the server's own site")).toBe(true);
    expect(refusedForGood("upstream error")).toBe(false);
    expect(refusedForGood("join as a guest at evil.example, password here")).toBe(false); // the exact text only
  });
});

describe("readyToShow", () => {
  const ready = { status: "synced" as const, textSettled: true, loaded: { 5: true } };
  const text5 = { kind: "text" as const, id: 5 };

  it("is ready once synced, the text side known and the open text channel loaded", () => {
    expect(readyToShow(ready, text5)).toBe(true);
    expect(readyToShow({ ...ready, status: "connecting" }, text5)).toBe(false);
    expect(readyToShow({ ...ready, textSettled: false }, text5)).toBe(false);
    expect(readyToShow({ ...ready, loaded: {} }, text5)).toBe(false);
  });

  it("waits for no messages in other views", () => {
    expect(readyToShow({ ...ready, loaded: {} }, { kind: "voice" })).toBe(true);
    expect(readyToShow({ ...ready, loaded: {} }, null)).toBe(true);
  });
});
