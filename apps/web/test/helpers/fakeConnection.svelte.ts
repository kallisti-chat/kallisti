import { initialState, type ServerState } from "@kallisti/protocol";

// A connection whose status a test sets (reactive, as the real one).
// close() only asks, as a WebSocket does: closed comes with drop().
export const fakeConnection = (url: string) => {
  let state = $state.raw<ServerState>(initialState);
  let closeRequests = 0;
  let closed = $state(false);
  let wasSynced = $state(false);
  let closeReason = $state("");
  let failedToOpen = $state(false);
  let textSettled = $state(false);
  let loaded = $state.raw<Record<number, boolean>>({});
  const historyRequests: number[] = [];
  let aliveChecks = 0;
  return {
    url,
    get state() {
      return state;
    },
    get closed() {
      return closed;
    },
    get closeReason() {
      return closeReason;
    },
    get wasSynced() {
      return wasSynced;
    },
    get failedToOpen() {
      return failedToOpen;
    },
    // text: the text side is known with the sync (else see settleText).
    sync(text = true) {
      state = { ...state, status: "synced" };
      wasSynced = true;
      textSettled = text;
    },
    get textSettled() {
      return textSettled;
    },
    settleText() {
      textSettled = true;
    },
    get text() {
      return { loaded };
    },
    load(channel: number) {
      loaded = { ...loaded, [channel]: true };
    },
    historyRequests,
    requestHistory(channel: number) {
      historyRequests.push(channel);
    },
    get aliveChecks() {
      return aliveChecks;
    },
    checkAlive() {
      aliveChecks++;
    },
    drop(reason = "") {
      closeReason = reason;
      closed = true;
    },
    // The socket closed before it opened (no reason: the browser refused it).
    failToOpen() {
      failedToOpen = true;
      closed = true;
    },
    get closeRequests() {
      return closeRequests;
    },
    close() {
      closeRequests++;
    },
    setAudio() {},
  };
};
export type FakeConnection = ReturnType<typeof fakeConnection>;
