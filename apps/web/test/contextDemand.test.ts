import { describe, expect, it, vi } from "vitest";
import { createContextDemand, clientAudioDemand } from "../src/lib/voice/contextDemand";
import { startAfter } from "../src/lib/voice/worklet";
import { swapContext } from "../src/lib/voice/restart";

const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
const deferred = () => {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => (resolve = r));
  return { promise, resolve };
};
const context = (state = "running") => {
  const ctx = {
    state,
    suspend: vi.fn(async () => { ctx.state = "suspended"; }),
    resume: vi.fn(async () => { ctx.state = "running"; }),
  };
  return ctx;
};

describe("shared context demand", () => {
  it("suspends idle output after the flush; idle gesture wakes do nothing", async () => {
    const demand = createContextDemand();
    const ctx = context();
    demand.setContext(ctx);
    demand.wake();
    expect(ctx.suspend).not.toHaveBeenCalled();
    await flush();
    expect(ctx.suspend).toHaveBeenCalledOnce();
    demand.wake();
    expect(ctx.resume).not.toHaveBeenCalled();
  });

  it("explicitly suspends a new idle context even before its automatic startup completes", async () => {
    const demand = createContextDemand();
    const ctx = context("suspended"); // a browser can still start this context later
    demand.setContext(ctx);
    await flush();
    expect(ctx.suspend).toHaveBeenCalledOnce();
    expect(ctx.resume).not.toHaveBeenCalled();
  });

  it("keeps demand from all owners, and releases only the last one", async () => {
    const demand = createContextDemand();
    const ctx = context();
    const a = {}, b = {};
    demand.setContext(ctx);
    demand.set(a, true);
    demand.set(b, false);
    await flush();
    expect(ctx.suspend).not.toHaveBeenCalled();
    demand.set(b, true);
    demand.release(a);
    await flush();
    expect(ctx.suspend).not.toHaveBeenCalled();
    demand.release(b);
    await flush();
    expect(ctx.suspend).toHaveBeenCalledOnce();
  });

  it("coalesces a release and acquisition in the same flush", async () => {
    const demand = createContextDemand();
    const ctx = context();
    const a = {};
    demand.set(a, true);
    demand.setContext(ctx);
    demand.release(a);
    demand.set({}, true);
    await flush();
    expect(ctx.suspend).not.toHaveBeenCalled();
  });

  it("resumes synchronously on demand and retries on a gesture after interruption", async () => {
    const demand = createContextDemand();
    const ctx = context("suspended");
    demand.setContext(ctx);
    demand.set({}, true);
    expect(ctx.resume).toHaveBeenCalledOnce();
    await flush();
    ctx.state = "interrupted";
    demand.wake();
    expect(ctx.resume).toHaveBeenCalledTimes(2);
  });

  it("retries in a gesture even when an autoplay resume is still pending", async () => {
    const demand = createContextDemand(), ctx = context("suspended"), first = deferred();
    ctx.resume.mockImplementationOnce(() => first.promise);
    demand.setContext(ctx);
    demand.set({}, true);
    expect(ctx.resume).toHaveBeenCalledOnce();
    demand.wake(); // a pending autoplay attempt must not consume this gesture
    expect(ctx.resume).toHaveBeenCalledTimes(2);
    first.resolve();
    await flush();
  });

  it("preserves demand through replacement, before the server confirms a join", async () => {
    const demand = createContextDemand();
    const old = context("suspended"), next = context("suspended");
    demand.setContext(old);
    demand.set({}, true); // the click, before restartAudio
    const make = () => { demand.setContext(next); return next; };
    expect(swapContext(old, make, () => {}, () => {})).toBe(next);
    expect(next.resume).toHaveBeenCalledOnce(); // still in the click
    await flush();
    expect(next.suspend).not.toHaveBeenCalled();
  });

  it("waits for an in-flight suspend even if state still says running", async () => {
    const demand = createContextDemand();
    const ctx = context();
    const suspended = deferred();
    ctx.suspend.mockImplementationOnce(async () => { await suspended.promise; ctx.state = "suspended"; });
    demand.setContext(ctx);
    await flush();
    expect(ctx.state).toBe("running");
    demand.set({}, true);
    demand.wake();
    expect(ctx.resume).not.toHaveBeenCalled();
    suspended.resolve();
    await flush();
    expect(ctx.resume).toHaveBeenCalledOnce();
    expect(ctx.state).toBe("running");
  });

  it("does not resume after demand ends or the context is replaced during suspend", async () => {
    const demand = createContextDemand();
    const ctx = context(), next = context();
    const suspended = deferred(), owner = {};
    ctx.suspend.mockImplementationOnce(() => suspended.promise);
    demand.setContext(ctx);
    await flush();
    demand.set(owner, true);
    demand.release(owner);
    demand.setContext(next);
    suspended.resolve();
    await flush();
    expect(ctx.resume).not.toHaveBeenCalled();
    expect(next.suspend).toHaveBeenCalledOnce();
  });

  it.each([false, true])("holds Linux output through module load and suspend completion (failure: %s)", async (fail) => {
    const demand = createContextDemand();
    const ctx = context(), module = deferred(), suspended = deferred();
    ctx.suspend.mockImplementationOnce(async () => { await suspended.promise; ctx.state = "suspended"; });
    const ready = startAfter(ctx, module.promise.then(() => { if (fail) throw new Error("no worklet"); }), () => {});
    demand.setContext(ctx, ready);
    demand.set({}, true);
    demand.wake();
    module.resolve();
    await flush();
    expect(ctx.resume).not.toHaveBeenCalled();
    suspended.resolve();
    await flush();
    expect(ctx.resume).toHaveBeenCalledOnce();
  });

  it("stays idle when Linux startup finishes, and ignores old readiness", async () => {
    const demand = createContextDemand();
    const old = context(), ctx = context(), module = deferred();
    demand.setContext(old, module.promise);
    demand.setContext(ctx, startAfter(ctx, Promise.resolve(), () => {}));
    await flush();
    expect(ctx.resume).not.toHaveBeenCalled();
    demand.set({}, true);
    module.resolve();
    await flush();
    expect(old.resume).not.toHaveBeenCalled();
    expect(ctx.resume).toHaveBeenCalledOnce();
  });

  it("ignores closed contexts and catches rejected operations", async () => {
    const demand = createContextDemand();
    const ctx = context("closed"), owner = {};
    demand.setContext(ctx);
    demand.set(owner, true);
    demand.wake();
    await flush();
    expect(ctx.resume).not.toHaveBeenCalled();
    ctx.state = "suspended";
    ctx.resume.mockRejectedValueOnce(new Error("blocked"));
    demand.wake();
    await flush();
    demand.wake();
    expect(ctx.resume).toHaveBeenCalledTimes(2);
    await flush();
    ctx.suspend.mockRejectedValueOnce(new Error("closed meanwhile"));
    demand.release(owner);
    await flush();
  });
});

describe("Client audio demand", () => {
  const idle = { voice: false, rejoining: null, pendingJoin: null, micOn: false, streams: null };
  it("includes voice, rejoining (including root), pending join and settings microphone", () => {
    expect(clientAudioDemand(idle)).toBe(false);
    for (const change of [{ voice: true }, { rejoining: 0 }, { pendingJoin: 5 }, { micOn: true }]) {
      expect(clientAudioDemand({ ...idle, ...change })).toBe(true);
    }
  });
  it("keeps the stream clock for configured sound, even zero volume or deafen", () => {
    const stream = (volume: number | undefined, watching = [7]) => ({ watching, volumeOf: () => volume });
    expect(clientAudioDemand({ ...idle, streams: stream(0) })).toBe(true);
    expect(clientAudioDemand({ ...idle, streams: stream(1) })).toBe(true);
    expect(clientAudioDemand({ ...idle, streams: stream(undefined) })).toBe(false);
    expect(clientAudioDemand({ ...idle, streams: stream(1, []) })).toBe(false);
  });
});
