// worker-safe proof of work: no UI, translations or reactive state.
export type Challenge = { challenge: string; bits: number };
export type Proof = { challenge: string; nonce: string };

export const zeroBits = (hash: Uint8Array) => {
  let n = 0;
  for (const b of hash) {
    if (b !== 0) return n + Math.clz32(b) - 24;
    n += 8;
  }
  return n;
};

// many digests at once: each one alone waits for its promise.
export const solve = async (challenge: string, bits: number, batch = 512) => {
  const enc = new TextEncoder();
  for (let start = 0; ; start += batch) {
    const nonces = Array.from({ length: batch }, (_, i) => String(start + i));
    const hashes = await Promise.all(nonces.map((n) => crypto.subtle.digest("SHA-256", enc.encode(challenge + n))));
    const i = hashes.findIndex((h) => zeroBits(new Uint8Array(h)) >= bits);
    if (i >= 0) return nonces[i]!;
  }
};

