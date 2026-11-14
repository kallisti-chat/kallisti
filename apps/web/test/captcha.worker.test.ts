import { webcrypto } from "node:crypto";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";
import { build } from "vite";
import { beforeAll, describe, expect, it } from "vitest";

let worker: string;
beforeAll(async () => {
  const result = await build({
    configFile: false,
    logLevel: "silent",
    build: { write: false, rolldownOptions: { input: resolve(import.meta.dirname, "fixtures/captcha-worker-entry.ts") } },
  });
  const output = Array.isArray(result) ? result[0]! : result;
  if (!("output" in output)) throw new Error("expected a completed build");
  const asset = output.output.find((item) => item.type === "asset" && /captcha\.worker-.*\.js$/.test(item.fileName));
  if (!asset || asset.type !== "asset") throw new Error("missing bundled captcha worker");
  worker = typeof asset.source === "string" ? asset.source : new TextDecoder().decode(asset.source);
});

const runWorker = (crypto: unknown, challenge: string, bits: number) => new Promise<string | null>((resolve, reject) => {
  const scope = { self: { postMessage: resolve }, crypto, TextEncoder, onmessage: undefined as undefined | ((event: unknown) => void) };
  try {
    runInNewContext(worker, scope, { timeout: 1000 });
    if (!scope.onmessage) throw new Error("worker did not install a message handler");
    scope.onmessage({ data: { challenge, bits } });
  } catch (error) {
    reject(error);
  }
});

describe("production captcha worker", () => {
  it("starts without UI globals and returns a valid proof", async () => {
    const challenge = "production-browser-check";
    const nonce = await runWorker(webcrypto, challenge, 10);
    expect(typeof nonce).toBe("string");
    const digest = new Uint8Array(await webcrypto.subtle.digest("SHA-256", new TextEncoder().encode(challenge + nonce)));
    expect(digest[0]).toBe(0);
    expect(digest[1]! & 0xc0).toBe(0);
  });

  it("reports unavailable cryptography without hanging", async () => {
    expect(await runWorker(undefined, "challenge", 1)).toBeNull();
  });
});
