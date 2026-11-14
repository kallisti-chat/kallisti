// Solves one challenge (see captcha.ts); null: it cannot (no crypto.subtle
// outside secure contexts, for example).
import { solve, type Challenge } from "./captchaSolver";

const post = (nonce: string | null) => (self as unknown as Worker).postMessage(nonce);
onmessage = (e: MessageEvent<Challenge>) => void solve(e.data.challenge, e.data.bits).then(post, () => post(null));
