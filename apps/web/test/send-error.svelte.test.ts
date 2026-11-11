// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
vi.mock("../src/lib/voice/capture.svelte", () => ({ createCapture: vi.fn() }));
vi.mock("../src/lib/voice/playback.svelte", () => ({ createPlayback: vi.fn() }));
vi.mock("../src/lib/stream/client.svelte", () => ({ createStreamClient: () => null, streamingSupported: () => false }));
import { connect, type Connection } from "../src/lib/connection.svelte";
import { Socket } from "./helpers/socket";

const lengthError = "message must be 1 to 2000 characters";
const opened: Connection[] = [];
const show = () => {
  vi.stubGlobal("WebSocket", Socket);
  const connection = connect("wss://example.test", { username: "me" }, null);
  opened.push(connection);
  return { connection, ws: Socket.latest };
};
const send = (c: Connection, mode: "plain" | "confirmed") => mode === "plain"
  ? c.sendToTextChannel(1, "short message", 7, true)
  : c.sendConfirmed(1, "short message", "retry", [], 7, true);
const message = { id: 1, channel: 1, author: "me", text: "short message", at: 1 };
afterEach(() => { for (const c of opened.splice(0)) c.close(); vi.unstubAllGlobals(); });

it.each(["plain", "confirmed"] as const)("clears a stale length error on a new %s send and stays clear after acceptance", async (mode) => {
  const { connection: c, ws } = show();
  c.sendToTextChannel(1, "x".repeat(2001));
  expect(JSON.parse(ws.sent[0]!).text).toHaveLength(2001); // Length enforcement stays on the server.
  ws.reply({ type: "error", message: lengthError });
  expect(c.text.lastError).toBe(lengthError);
  const pending = send(c, mode);
  const errorDuringSend = c.text.lastError;
  expect(JSON.parse(ws.sent.at(-1)!)).toMatchObject({ type: "send", channel: 1, text: "short message", reply: 7, noPing: true });
  ws.reply({ type: "message", message, ...(mode === "confirmed" ? { nonce: "retry" } : {}) });
  await pending;
  expect(errorDuringSend).toBeNull();
  expect(c.text.lastError).toBeNull();
  expect(c.text.messages[1]).toHaveLength(1);
});

it.each(["plain", "confirmed"] as const)("shows a new rejection after a %s retry", async (mode) => {
  const { connection: c, ws } = show();
  ws.reply({ type: "error", message: lengthError });
  const pending = send(c, mode);
  const failure = mode === "confirmed" ? expect(pending).rejects.toThrow("try again") : undefined;
  const errorDuringSend = c.text.lastError;
  ws.reply({ type: "error", message: "try again", ...(mode === "confirmed" ? { nonce: "retry" } : {}) });
  await failure;
  expect(errorDuringSend).toBeNull();
  expect(c.text.lastError).toBe("try again");
});

it("does not dismiss the error for an unrelated incoming message", () => {
  const { connection: c, ws } = show();
  ws.reply({ type: "error", message: lengthError });
  ws.reply({ type: "message", message: { ...message, author: "other" } });
  expect(c.text.lastError).toBe(lengthError);
});

it.each(["plain", "confirmed"] as const)("keeps the error when a disconnected %s send cannot be attempted", async (mode) => {
  const { connection: c, ws } = show();
  ws.reply({ type: "error", message: lengthError });
  c.close(); ws.readyState = 3;
  const before = ws.sent.length;
  if (mode === "confirmed") await expect(send(c, mode)).rejects.toThrow();
  else send(c, mode);
  expect(c.text.lastError).toBe(lengthError);
  expect(ws.sent).toHaveLength(before);
});
