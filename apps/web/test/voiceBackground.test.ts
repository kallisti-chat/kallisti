import { describe, expect, it, vi } from "vitest";
import { keepVoiceRunning } from "../src/lib/voice/background";

const fakeDoc = () => {
  const listeners = new Set<() => void>();
  return {
    visibilityState: "visible" as DocumentVisibilityState,
    addEventListener: (_: "visibilitychange", l: () => void) => void listeners.add(l),
    removeEventListener: (_: "visibilitychange", l: () => void) => void listeners.delete(l),
    hide() {
      this.visibilityState = "hidden";
      listeners.forEach((l) => l());
    },
    show() {
      this.visibilityState = "visible";
      listeners.forEach((l) => l());
    },
    listeners,
  };
};

const fakeCtx = (state: string) => {
  const listeners = new Set<() => void>();
  const ctx = {
    state,
    resume: vi.fn(async () => void (ctx.state = "running")),
    addEventListener: (_: "statechange", l: () => void) => void listeners.add(l),
    removeEventListener: (_: "statechange", l: () => void) => void listeners.delete(l),
    set(next: string) {
      ctx.state = next;
      listeners.forEach((l) => l());
    },
    listeners,
  };
  return ctx;
};

describe("keepVoiceRunning", () => {
  it("resumes an interruption while the window is visible", () => {
    const doc = fakeDoc();
    const ctx = fakeCtx("running");
    keepVoiceRunning(doc, ctx);
    ctx.set("interrupted");
    expect(ctx.resume).toHaveBeenCalledOnce();
    expect(ctx.state).toBe("running");
  });

  it("recovers a context already interrupted when voice reconnects", () => {
    const ctx = fakeCtx("interrupted");
    keepVoiceRunning(fakeDoc(), ctx);
    expect(ctx.resume).toHaveBeenCalledOnce();
    expect(ctx.state).toBe("running");
  });

  it("retries on returning to the window after a failed background resume", async () => {
    const doc = fakeDoc();
    const ctx = fakeCtx("running");
    keepVoiceRunning(doc, ctx);
    doc.hide();
    ctx.resume.mockRejectedValueOnce(new Error("audio unavailable"));
    ctx.set("interrupted");
    await Promise.resolve();
    doc.show();
    expect(ctx.resume).toHaveBeenCalledTimes(2);
    expect(ctx.state).toBe("running");
  });

  it("retries when the page becomes hidden if the first attempt did not resume it", async () => {
    const doc = fakeDoc();
    const ctx = fakeCtx("running");
    keepVoiceRunning(doc, ctx);
    ctx.resume.mockResolvedValueOnce(undefined);
    ctx.set("interrupted");
    await Promise.resolve();
    expect(ctx.state).toBe("interrupted");
    doc.hide();
    expect(ctx.resume).toHaveBeenCalledTimes(2);
    expect(ctx.state).toBe("running");
  });

  it("resumes a context interrupted while the page is hidden", () => {
    const doc = fakeDoc();
    const ctx = fakeCtx("running");
    keepVoiceRunning(doc, ctx);
    doc.hide();
    ctx.set("interrupted");
    expect(ctx.resume).toHaveBeenCalledOnce();
  });

  it("leaves running, suspended and closed contexts alone", () => {
    const doc = fakeDoc();
    for (const state of ["running", "suspended", "closed"]) {
      const ctx = fakeCtx(state);
      const stop = keepVoiceRunning(doc, ctx);
      doc.hide();
      doc.show();
      expect(ctx.resume).not.toHaveBeenCalled();
      stop();
    }
  });

  it("stops listening when stopped", () => {
    const doc = fakeDoc();
    const ctx = fakeCtx("running");
    const stop = keepVoiceRunning(doc, ctx);
    stop();
    expect(doc.listeners.size).toBe(0);
    expect(ctx.listeners.size).toBe(0);
    ctx.set("interrupted");
    doc.hide();
    expect(ctx.resume).not.toHaveBeenCalled();
  });
});
