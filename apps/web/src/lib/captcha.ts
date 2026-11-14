// The server's browser check before a new account or guest account (see
// server/internal/auth/challenge.go): find a nonce so that
// SHA-256(challenge + nonce) starts with `bits` zero bits. A worker does
// it (captcha.worker.ts), so the page stays responsive.
import { t } from "./i18n.svelte";
import type { Challenge, Proof } from "./captchaSolver";
export type { Challenge, Proof } from "./captchaSolver";
export { solve, zeroBits } from "./captchaSolver";

// The admins' setting (see ModerationDialog).
export const CAPTCHA_LEVELS = ["off", "normal", "hard"] as const;
export type CaptchaLevel = (typeof CAPTCHA_LEVELS)[number];
export const isCaptchaLevel = (v: unknown): v is CaptchaLevel => CAPTCHA_LEVELS.includes(v as CaptchaLevel);

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
