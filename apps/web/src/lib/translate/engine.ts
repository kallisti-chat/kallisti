import { BatchTranslator, TranslatorBacking } from "@mkljczk/bergamot-translator/translator.js";
import { reachable, registryOf, type Models } from "./languages";
import type { Context } from "./prompt";
import type { Loading } from "./llm";

// Translating a message on this device: the browser's own translator
// (Translator API: desktop Chrome and Edge), else Bergamot (Firefox's
// engine, WebAssembly in a worker) with the server's models (see the
// server's translation package; fetched there once, checked, kept).
// Loaded only on the first translation.

// No models for the language on the server.
export class Unsupported extends Error {
  readonly unsupported = true;
}

type BuiltIn = {
  availability(o: { sourceLanguage: string; targetLanguage: string }): Promise<string>;
  create(o: { sourceLanguage: string; targetLanguage: string }): Promise<{ translate(text: string): Promise<string> }>;
};

// A promise, or null after ms (some browsers never answer).
const within = <T>(p: Promise<T>, ms: number) => Promise.race([p, new Promise<null>((ok) => setTimeout(() => ok(null), ms))]);

// Only with the pair already on this device: a download would come from the
// browser's maker (who would see which languages), and Bergamot's come from
// our server.
// A translator per pair (null: not on this device), made once.
const builtIns = new Map<string, Promise<{ translate(text: string): Promise<string> } | null>>();

const builtIn = async (text: string, from: string, to: string): Promise<string | null> => {
  const api = (globalThis as { Translator?: BuiltIn }).Translator;
  if (!api) return null;
  const pair = `${from}-${to}`;
  if (!builtIns.has(pair)) {
    const options = { sourceLanguage: from, targetLanguage: to };
    const made = (async () => ((await within(api.availability(options), 3000)) === "available" ? within(api.create(options), 10_000) : null))();
    builtIns.set(pair, made.catch(() => null));
  }
  try {
    const translator = await builtIns.get(pair)!;
    return translator && (await within(translator.translate(text), 30_000));
  } catch {
    return null; // Bergamot then
  }
};

// Bergamot's downloads in progress, by URL, to whoever waits (see download).
const downloads = new Map<string, { loaded: number; total: number }>();
const waiting = new Set<(l: Loading) => void>();
const report = () => {
  const [loaded, total] = [...downloads.values()].reduce(([l, t], d) => [l + d.loaded, t + d.total], [0, 0]);
  for (const f of waiting) f({ download: total ? Math.floor((100 * loaded) / total) : 0 });
};

const hex = (b: ArrayBuffer) => [...new Uint8Array(b)].map((x) => x.toString(16).padStart(2, "0")).join("");

// A model file, read in pieces (its progress) and checked by its SHA-256.
const download = async (url: string, sha256: string | undefined, total: number) => {
  const res = await fetch(url, { credentials: "omit" });
  if (!res.ok || !res.body) throw new Error(`${url}: ${res.status}`);
  const parts: Uint8Array[] = [];
  const reader = res.body.getReader();
  downloads.set(url, { loaded: 0, total });
  try {
    for (let r = await reader.read(); !r.done; r = await reader.read()) {
      parts.push(r.value);
      downloads.get(url)!.loaded += r.value.length;
      report();
    }
  } finally {
    downloads.delete(url);
  }
  const data = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  parts.reduce((at, p) => (data.set(p, at), at + p.length), 0);
  if (sha256 && hex(await crypto.subtle.digest("SHA-256", data)) !== sha256) throw new Error(`${url}: wrong SHA-256`);
  return data.buffer;
};

// One Bergamot per server (its models), with the pairs it has; failed: the
// worker reported an error (it then leaves requests waiting).
type Bergamot = { translator: BatchTranslator; pairs: Set<string>; failed: Promise<never> };
const bergamots = new Map<string, Promise<Bergamot>>();

const bergamot = (base: string) => {
  let b = bergamots.get(base);
  if (!b) {
    b = (async () => {
      const res = await fetch(`${base}/api/translation/models`, { credentials: "omit" });
      if (!res.ok) throw new Error(`models: ${res.status}`);
      const models = (await res.json()) as Models;
      const registry = registryOf(models, base);
      const sizes = new Map(registry.flatMap((m) => Object.values(m.files).map((f): [string, number] => [f.name, f.size])));
      let fail: (e: Error) => void = () => {};
      const failed = new Promise<never>((_, reject) => (fail = reject));
      failed.catch(() => {});
      const onerror = (e: Error) => {
        console.error("translation worker", e);
        fail(e);
      };
      class Backing extends TranslatorBacking {
        override loadModelRegistry() {
          return Promise.resolve(registry);
        }
        override fetch(url: string, checksum: string | undefined) {
          return download(url, checksum, sizes.get(url) ?? 0);
        }
      }
      // The worker is the app's own file (workers must be same-origin).
      // onerror set afterwards: the options go to the worker (no functions).
      const backing = new Backing({ workerUrl: new URL("/bergamot/translator-worker.js", location.href).href, downloadTimeout: 0 });
      backing.onerror = onerror;
      const translator = new BatchTranslator({ workers: 1 }, backing);
      translator.onerror = onerror;
      return { translator, pairs: new Set(Object.keys(models.pairs)), failed };
    })();
    b.catch(() => bergamots.delete(base)); // asked again next time
    bergamots.set(base, b);
  }
  return b;
};

// The translation of a message's text from its language into `to`: with
// the larger model when asked (better, see llm.ts; the messages before as
// context), else or when it fails as below. onProgress: the models'
// download.
export type Options = { better?: boolean; context?: Context; onProgress?: (l: Loading) => void };

export const translate = async (text: string, from: string, to: string, base: string, options: Options = {}): Promise<string> => {
  if (options.better) {
    const { translateLLM, webGPU } = await import("./llm");
    if (await webGPU())
      try {
        return await translateLLM(text, from, to, base, options.context ?? { before: [], names: [] }, options.onProgress);
      } catch (e) {
        console.error("better translation", e);
      }
  }
  const own = await builtIn(text, from, to);
  if (own !== null) return own;
  const { translator, pairs, failed } = await bergamot(base);
  if (!reachable(from, to, pairs)) throw new Unsupported();
  if (options.onProgress) waiting.add(options.onProgress);
  try {
    // At once when the worker fails, else when it answers (or in time).
    const res = await Promise.race([within(translator.translate({ from, to, text, html: false }), 600_000), failed]);
    if (!res) throw new Error("translation timed out");
    return res.target.text;
  } catch (e) {
    // A worker that did not load stays so: a new one next time.
    bergamots.delete(base);
    void Promise.resolve()
      .then(() => translator.delete())
      .catch(() => {});
    throw e;
  } finally {
    if (options.onProgress) waiting.delete(options.onProgress);
  }
};
