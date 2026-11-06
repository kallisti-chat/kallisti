import { promptFor, type Context } from "./prompt";

// "Better translation": Hy-MT2-1.8B (Tencent's translation model, ONNX
// q4f16) with transformers.js on WebGPU. The model comes from our server
// (see the server's translation package: /api/translation/llm/<file>), the
// ONNX runtime's WebAssembly from the app's own files (its JavaScript is in
// the bundle): nothing from a CDN. The browser keeps
// the model in its cache. Loaded only on the first translation.

type Message = { role: string; content: string };
type Generator = (
  messages: Message[],
  options: Record<string, unknown>,
) => Promise<{ generated_text: Message[] }[]>;
type Progress = { status: string; file?: string; loaded?: number; total?: number };

type GPU = { requestAdapter(): Promise<{ features: Set<string> } | null> };

// WebGPU with 16-bit floats (the model is q4f16; some GPUs lack them). Asked
// once, before any download.
let usable: Promise<boolean> | null = null;
export const webGPU = () =>
  (usable ??= (async () => {
    const gpu = typeof navigator === "undefined" ? undefined : (navigator as { gpu?: GPU }).gpu;
    try {
      return !!(await gpu?.requestAdapter())?.features.has("shader-f16");
    } catch {
      return false;
    }
  })());

// The download in percent, to whoever waits for the model.
const waiting = new Set<(percent: number) => void>();
const files = new Map<string, { loaded: number; total: number }>();
const progress = (p: Progress) => {
  if (p.status !== "progress" || !p.file || !p.total) return;
  files.set(p.file, { loaded: p.loaded ?? 0, total: p.total });
  const [loaded, total] = [...files.values()].reduce(([l, t], f) => [l + f.loaded, t + f.total], [0, 0]);
  for (const f of waiting) f(Math.floor((100 * loaded) / total));
};

// One model for the page, from the first server it is asked from.
let loading: Promise<Generator> | null = null;
const load = (base: string) =>
  (loading ??= (async () => {
    files.clear(); // a new try counts from 0
    const { env, pipeline } = await import("@huggingface/transformers");
    const own = (path: string) => new URL(path, location.href).href;
    env.allowLocalModels = false;
    env.remoteHost = own(`${base}/api/translation/`);
    env.remotePathTemplate = "{model}/";
    // The runtime's own WebAssembly (an asset of the build) instead of the
    // CDN's that transformers.js sets; no cache of it (a blob: import, which
    // the Content Security Policy does not allow).
    env.useWasmCache = false;
    env.backends.onnx.wasm!.wasmPaths = undefined;
    const pipe = await pipeline("text-generation", "llm", { device: "webgpu", dtype: "q4f16", progress_callback: progress });
    return pipe as unknown as Generator;
  })().catch((e) => {
    loading = null; // asked again next time
    throw e;
  }));

export const translateLLM = async (text: string, from: string, to: string, base: string, context: Context, onProgress?: (percent: number) => void) => {
  if (onProgress) waiting.add(onProgress);
  let generate: Generator;
  try {
    generate = await load(base);
  } finally {
    if (onProgress) waiting.delete(onProgress);
  }
  const out = await generate([{ role: "user", content: promptFor(text, from, to, context) }], {
    max_new_tokens: Math.min(512, 2 * text.length + 64),
    do_sample: false,
    repetition_penalty: 1.05,
  });
  return out[0]!.generated_text.at(-1)!.content.trim();
};
