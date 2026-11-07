type Context = Pick<AudioContext, "suspend" | "resume"> & { readonly state: string };
type Output = { ctx: Context; ready: boolean; suspending: boolean };

// One owner per mounted Client. Never let one server's idle state stop
// another's audio. Only idle transitions wait for the effect flush.
export const createContextDemand = () => {
  const owners = new Set<object>();
  let output: Output | null = null;
  let queued = false;
  const wake = () => {
    const o = output;
    if (!o || !owners.size || !o.ready || o.suspending || o.ctx.state === "closed" || o.ctx.state === "running") return;
    // An autoplay resume can stay pending until a gesture. Do not let
    // that promise prevent another synchronous attempt in that gesture.
    void o.ctx.resume().catch(() => {}).then(() => {
      if (output === o && !owners.size) idle();
    });
  };
  const idle = () => {
    if (queued) return;
    queued = true;
    queueMicrotask(() => {
      queued = false;
      const o = output;
      if (!o || owners.size || !o.ready || o.suspending || o.ctx.state === "closed") return;
      // A new context may still start automatically after reporting suspended.
      o.suspending = true;
      void o.ctx.suspend().catch(() => {}).then(() => {
        o.suspending = false;
        // state can still be running when demand arrives during suspend.
        // Resume only after that operation settles, on the current output.
        if (output === o) wake();
      });
    });
  };
  return {
    setContext: (ctx: Context | null, ready?: Promise<unknown>) => {
      const o = output = ctx ? { ctx, ready: !ready, suspending: false } : null;
      if (o && ready) {
        void ready.catch(() => {}).then(() => {
          if (output !== o) return;
          o.ready = true;
          if (owners.size) wake();
          else idle();
        });
      } else if (owners.size) wake();
      else idle();
    },
    set: (owner: object, on: boolean) => {
      if (on) { owners.add(owner); wake(); }
      else { owners.delete(owner); idle(); }
    },
    release: (owner: object) => { owners.delete(owner); idle(); },
    wake,
  };
};
export type ContextDemand = ReturnType<typeof createContextDemand>;

// Stream sound keeps the video clock advancing even at zero gain/deafen.
export const clientAudioDemand = (s: {
  voice: boolean;
  rejoining: number | null;
  pendingJoin: number | null;
  micOn: boolean;
  streams: { watching: readonly number[]; volumeOf: (id: number) => number | undefined } | null;
}): boolean => {
  const { streams } = s;
  return s.voice || s.rejoining !== null || s.pendingJoin !== null || s.micOn ||
    !!streams?.watching.some((id) => streams.volumeOf(id) !== undefined);
};
