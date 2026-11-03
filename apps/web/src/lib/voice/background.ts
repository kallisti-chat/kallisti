// WebKit can interrupt an audio context without disconnecting voice.
// Recover whether the window is visible or hidden, including a context
// kept across a reconnect. On iOS a background transition may need another
// attempt once the page counts as hidden. Only while in voice
// (Client.svelte): a running context keeps the app awake.

type Doc = {
  visibilityState: DocumentVisibilityState;
  addEventListener: (type: "visibilitychange", listener: () => void) => void;
  removeEventListener: (type: "visibilitychange", listener: () => void) => void;
};
type Ctx = {
  readonly state: string; // "interrupted" is newer than TypeScript's list
  resume: () => Promise<void>;
  addEventListener: (type: "statechange", listener: () => void) => void;
  removeEventListener: (type: "statechange", listener: () => void) => void;
};

// Returns stop. An audio restart: stop, then again with the new context.
export const keepVoiceRunning = (doc: Doc, ctx: Ctx): (() => void) => {
  const check = () => {
    // Leave suspended contexts to the click handler: they may be waiting
    // for user activation, or the Linux app's worklet startup hold.
    if (ctx.state === "interrupted") ctx.resume().catch(() => {});
  };
  // Retry on either visibility transition if the previous attempt could
  // not resume audio. No timer loop while the OS still owns the device.
  doc.addEventListener("visibilitychange", check);
  ctx.addEventListener("statechange", check);
  check();
  return () => {
    doc.removeEventListener("visibilitychange", check);
    ctx.removeEventListener("statechange", check);
  };
};
