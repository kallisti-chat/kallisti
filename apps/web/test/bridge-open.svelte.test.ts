// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
vi.mock("../src/lib/voice/capture.svelte", () => ({ createCapture: vi.fn() }));
vi.mock("../src/lib/voice/playback.svelte", () => ({ createPlayback: vi.fn() }));
vi.mock("../src/lib/stream/client.svelte", () => ({ createStreamClient: () => null, streamingSupported: () => false }));
import { connect } from "../src/lib/connection.svelte";
import { Socket } from "./helpers/socket";

// failedToOpen: the socket closed before it opened, and not by us (the
// apps' bridge was unreachable, see lib/desktop bridgeFailed).
const start = () => {
  vi.stubGlobal("WebSocket", Socket);
  const connection = connect("ws://127.0.0.1:1/?kallisti", { username: "me" }, null);
  return { connection, ws: Socket.latest as Socket & { onopen?: () => void } };
};
afterEach(() => vi.unstubAllGlobals());

it("failed to open: closed before it opened", () => {
  const { connection, ws } = start();
  expect(connection.failedToOpen).toBe(false);
  ws.onclose?.({ reason: "" });
  expect(connection.failedToOpen).toBe(true);
});

it("not failed: closed by us before it opened (a newer attempt, a wake)", () => {
  const { connection } = start();
  connection.close();
  expect(connection.closed).toBe(true);
  expect(connection.failedToOpen).toBe(false);
});

it("not failed: closed after it opened", () => {
  const { connection, ws } = start();
  ws.onopen?.();
  ws.onclose?.({ reason: "" });
  expect(connection.failedToOpen).toBe(false);
});
