// The server's browser check before a new account or guest account (see
// server/internal/auth/challenge.go): find a nonce so that
// SHA-256(challenge + nonce) starts with `bits` zero bits. A worker does
// it (captcha.worker.ts), so the page stays responsive.
import { t } from "./i18n.svelte";

export type Challenge = { challenge: string; bits: number };
export type Proof = { challenge: string; nonce: string };

// The admins' setting (see ModerationDialog).
export const CAPTCHA_LEVELS = ["off", "normal", "hard"] as const;
export type CaptchaLevel = (typeof CAPTCHA_LEVELS)[number];
export const isCaptchaLevel = (v: unknown): v is CaptchaLevel => CAPTCHA_LEVELS.includes(v as CaptchaLevel);

export const zeroBits = (hash: Uint8Array) => {
  let n = 0;
  for (const b of hash) {
    if (b !== 0) return n + Math.clz32(b) - 24;
    n += 8;
  }
  return n;
};

// Many digests at once: each one alone waits for its promise.
export const solve = async (challenge: string, bits: number, batch = 512) => {
  const enc = new TextEncoder();
  for (let start = 0; ; start += batch) {
    const nonces = Array.from({ length: batch }, (_, i) => String(start + i));
    const hashes = await Promise.all(nonces.map((n) => crypto.subtle.digest("SHA-256", enc.encode(challenge + n))));
    const i = hashes.findIndex((h) => zeroBits(new Uint8Array(h)) >= bits);
    if (i >= 0) return nonces[i]!;
  }
};

export const solveInWorker = (c: Challenge) =>
  new Promise<Proof>((resolve, reject) => {
    const worker = new Worker(new URL("./captcha.worker.ts", import.meta.url), { type: "module" });
    const fail = () => reject(new Error(t("admin.captchaFailed")));
    worker.onmessage = (e: MessageEvent<string | null>) => {
      worker.terminate();
      if (e.data === null) fail();
      else resolve({ challenge: c.challenge, nonce: e.data });
    };
    worker.onerror = () => (worker.terminate(), fail());
    worker.postMessage(c);
  });
