// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
vi.mock("../src/lib/voice/capture.svelte", () => ({ createCapture: vi.fn() }));
vi.mock("../src/lib/voice/playback.svelte", () => ({ createPlayback: vi.fn() }));
vi.mock("../src/lib/stream/client.svelte", () => ({ createStreamClient: () => null, streamingSupported: () => false }));
import { connect } from "../src/lib/connection.svelte";
import { Socket } from "./helpers/socket";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// GitHub #19: a token request refused by the rate limit failed silently
// after 10 s; now at once, with the server's message (the panel shows it).
it("fails a token request at once when the server refuses it over the rate limit", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  const c = connect("wss://example.test", { username: "me" }, null);
  const ws = Socket.latest;
  const download = c.downloadRecording("r1");
  // Another request's refusal is not this one's.
  ws.reply({ type: "error", message: "too many requests, slow down", about: "rate", request: "history" });
  ws.reply({ type: "error", message: "too many requests, slow down", about: "rate", request: "recording-token" });
  await expect(download).rejects.toThrow("too many requests, slow down");
  c.close();
});
