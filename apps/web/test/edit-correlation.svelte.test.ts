// @vitest-environment jsdom
import { expect, it, vi, afterEach } from "vitest";
vi.mock("../src/lib/voice/capture.svelte", () => ({ createCapture: vi.fn() }));
vi.mock("../src/lib/voice/playback.svelte", () => ({ createPlayback: vi.fn() }));
vi.mock("../src/lib/stream/client.svelte", () => ({ createStreamClient: () => null, streamingSupported: () => false }));
import { connect } from "../src/lib/connection.svelte";
import { Socket } from "./helpers/socket";
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });
it("matches edit results and early refusals by request AND message, not broadcasts or order", async () => {
  vi.stubGlobal("WebSocket", Socket);
  const c = connect("wss://example.test", { username: "me" }, null);
  const ws = Socket.latest;
  const first = c.editMessage(1, "one"), second = c.editMessage(2, "two");
  const [a, b] = ws.sent.map((s) => JSON.parse(s));
  const done = vi.fn(); void first.then(done);
  ws.reply({ type: "edit-result", requestId: a.requestId, message: 2 });
  ws.reply({ type: "error", requestId: b.requestId, editMessage: 1, message: "wrong message" });
  ws.reply({ type: "message-edited", message: { id: 1, channel: 1, author: "me", text: "one", at: 1 } });
  await Promise.resolve(); expect(done).not.toHaveBeenCalled();
  ws.reply({ type: "error", requestId: b.requestId, editMessage: 2, message: "timed out" });
  await expect(second).rejects.toThrow("timed out");
  ws.reply({ type: "edit-result", requestId: a.requestId, message: 1 });
  await first; expect(done).toHaveBeenCalledOnce();
  ws.reply({ type: "error", requestId: a.requestId, editMessage: 1, message: "late" });
  expect(done).toHaveBeenCalledOnce(); c.close();
});
it("retains a failure on no answer and ignores its late result during a retry", async () => {
  vi.useFakeTimers(); vi.stubGlobal("WebSocket", Socket);
  const c = connect("wss://example.test", { username: "me" }, null), ws = Socket.latest;
  const first = c.editMessage(1, "one"); const failed = expect(first).rejects.toThrow();
  const a = JSON.parse(ws.sent[0]!);
  await vi.advanceTimersByTimeAsync(15_000); await failed;
  const next = c.editMessage(1, "two"), done = vi.fn(); void next.then(done);
  const b = JSON.parse(ws.sent[1]!);
  expect(b.requestId).not.toBe(a.requestId);
  ws.reply({ type: "edit-result", requestId: a.requestId, message: 1 });
  await Promise.resolve(); expect(done).not.toHaveBeenCalled();
  ws.reply({ type: "edit-result", requestId: b.requestId, message: 1 });
  await next; expect(done).toHaveBeenCalledOnce(); c.close();
});
it("rejects a disconnected edit instead of reporting success", async () => {
  vi.stubGlobal("WebSocket", Socket);
  const c = connect("wss://example.test", { username: "me" }, null);
  const pending = c.editMessage(1, "draft");
  Socket.latest.close(); await expect(pending).rejects.toThrow();
  Socket.latest.readyState = 3;
  await expect(c.editMessage(1, "draft")).rejects.toThrow();
});
