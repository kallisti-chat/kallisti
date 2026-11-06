import { BatchTranslator, TranslatorBacking } from "@mkljczk/bergamot-translator/translator.js";
import { reachable, registryOf, type Models } from "./languages";

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

// One Bergamot per server (its models), with the pairs it has.
const bergamots = new Map<string, Promise<{ translator: BatchTranslator; pairs: Set<string> }>>();

const bergamot = (base: string) => {
  let b = bergamots.get(base);
  if (!b) {
    b = (async () => {
      const res = await fetch(`${base}/api/translation/models`, { credentials: "omit" });
      if (!res.ok) throw new Error(`models: ${res.status}`);
      const models = (await res.json()) as Models;
      const registry = registryOf(models, base);
      class Backing extends TranslatorBacking {
        override loadModelRegistry() {
          return Promise.resolve(registry);
        }
      }
      // The worker is the app's own file (workers must be same-origin).
      const backing = new Backing({ workerUrl: new URL("/bergamot/translator-worker.js", location.href).href, downloadTimeout: 0 });
      return { translator: new BatchTranslator({ workers: 1 }, backing), pairs: new Set(Object.keys(models.pairs)) };
    })();
    b.catch(() => bergamots.delete(base)); // asked again next time
    bergamots.set(base, b);
  }
  return b;
};

// The translation of a message's text from its language into `to`. At most
// a few minutes (the first time with the models' download): the worker
// leaves a request waiting when it cannot load.
export const translate = async (text: string, from: string, to: string, base: string): Promise<string> => {
  const own = await builtIn(text, from, to);
  if (own !== null) return own;
  const { translator, pairs } = await bergamot(base);
  if (!reachable(from, to, pairs)) throw new Unsupported();
  const res = await within(translator.translate({ from, to, text, html: false }), 300_000);
  if (!res) {
    // A worker that did not load stays so: a new one next time.
    bergamots.delete(base);
    void translator.delete();
    throw new Error("translation timed out");
  }
  return res.target.text;
};
