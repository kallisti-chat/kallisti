import { proto, type ServerState } from "@kallisti/protocol";
import { certificateProblem } from "./desktop";
import type { Place, View } from "./place";

// The server closed the connection because its session ended (a logout,
// also on another device, or a password change there). Same reason text as
// the bridge's Revoke.
// Also a token the server does not know (expired, or ended on its site).
export const loggedOut = (closeReason: string) => closeReason === "logged out" || closeReason === "session expired";

// Refused by the server's rules (a guest on another site): retrying cannot
// help. The bridge's reason text, exactly (another server cannot make the
// client show text of its own).
export const GUEST_ELSEWHERE = "join as a guest on the server's own site";
export const refusedForGood = (closeReason: string) => closeReason === GUEST_ELSEWHERE;

// Coming back by itself after a lost connection (a background tab, the
// network, a server restart): only for a connection that was in, and not
// after a kick, a refusal, a logout or a certificate question (the user
// decides). straightNext: the apps' bridge could not reach a Kallisti
// server, and the next try goes straight to it (see lib/desktop
// bridgeFailed).
export const shouldReconnect = (c: {
  status: ServerState["status"];
  closed: boolean;
  closeReason: string;
  wasSynced: boolean;
  straightNext?: boolean;
}) =>
  c.closed && (c.wasSynced || c.straightNext === true) && c.status !== "kicked" && c.status !== "rejected" &&
  !loggedOut(c.closeReason) &&
  !refusedForGood(c.closeReason) &&
  certificateProblem(c.closeReason) === null;

const DELAYS_MS = [1000, 2000, 5000, 10_000, 20_000, 30_000];

// The wait before attempt n (0: the first).
export const reconnectDelay = (attempt: number) => DELAYS_MS[Math.min(attempt, DELAYS_MS.length - 1)]!;

// The server is usually back within seconds: a loss stays quiet at first
// (the view as it was, a try each second), then a small sign, then the
// banner with the growing waits (counted from the first again).
export const QUIET_MS = 10_000;
const SIGN_MS = 2000;
const QUIET_RETRY_MS = 1000;
export const lossNotice = (lostForMs: number): "none" | "sign" | "banner" =>
  lostForMs < SIGN_MS ? "none" : lostForMs < QUIET_MS ? "sign" : "banner";
// loudAttempt: the attempts since the quiet time ended.
export const nextDelay = (lostForMs: number, loudAttempt: number) => (lostForMs < QUIET_MS ? QUIET_RETRY_MS : reconnectDelay(loudAttempt));

// After a loss, the new connection replaces the old view only once it can
// show the same (else an empty channel flashes): synced, the text side
// known, and the open text channel's messages loaded.
export const readyToShow = (c: { status: ServerState["status"]; textSettled: boolean; loaded: Readonly<Record<number, boolean>> }, view: View | null) =>
  c.status === "synced" && c.textSettled && (view?.kind !== "text" || !!c.loaded[view.id]);

// Refused because the name is in use: right after a drop, that is often our
// own old session, which the server has not let go yet. Worth a few tries.
export const busyName = (s: Pick<ServerState, "status" | "rejectType">) =>
  s.status === "rejected" && s.rejectType === proto.Reject_RejectType.UsernameInUse;

// After a reconnect: the voice channel to join again (null: none, already
// there, or the channel is gone). voiceOther: our account's session in
// voice now, if not this one. That may be our old session, which the
// server has not let go yet; another device that went into voice meanwhile
// keeps it (the user can still take voice with a click).
export const rejoinTarget = (back: Place | null, here: number, exists: (id: number) => boolean, voiceOther: number | null): number | null =>
  back?.voice != null && back.voice !== here && exists(back.voice) && (voiceOther === null || voiceOther === back.session) ? back.voice : null;
