// The capture worklet's module, added once per context (shared promise: the
// capture and the Linux app's early load in App wait for the same one).
const modules = new WeakMap<AudioContext, Promise<void>>();
export const loadCaptureWorklet = (ctx: AudioContext): Promise<void> => {
  let p = modules.get(ctx);
  if (!p) {
    p = ctx.audioWorklet.addModule(new URL("./capture.worklet.js", import.meta.url));
    p.catch(() => modules.delete(ctx)); // a failed load may be tried again
    modules.set(ctx, p);
  }
  return p;
};

// WebKitGTK (the Linux desktop app) stops and starts a running output when
// its context gets the first worklet module. Both wait on its main thread
// until GStreamer and the sound server answer, without a time limit: the
// page hangs for good if they never do. Called right after the context is
// made, this holds the output until `ready`, so it starts once, after it.
// A shared demand controller can supply its own start callback and use the
// returned promise as a readiness barrier (including the initial suspend).
export const startAfter = (ctx: Pick<AudioContext, "suspend" | "resume">, ready: Promise<unknown>, start: () => void | Promise<void> = () => ctx.resume()): Promise<void> =>
  Promise.all([ctx.suspend().catch(() => {}), ready.catch(() => {})])
    .then(start)
    .catch(() => {}); // closed meanwhile (a new login)
