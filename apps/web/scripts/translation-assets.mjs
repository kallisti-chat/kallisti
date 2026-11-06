// Copies the translation engines' workers and WebAssembly (see
// lib/translate) to public/: Bergamot's to public/bergamot/ (the worker
// loads them by their names, next to itself), and the ONNX runtime that
// transformers.js uses to public/ort/ (else it loads it from a CDN). Runs
// before builds; the page loads them only on the first translation.
import { cpSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
// A package's folder, from its entry (packages need not export package.json).
const packageDir = (req, name) => {
  let dir = dirname(req.resolve(name));
  while (!existsSync(join(dir, "package.json")) || JSON.parse(readFileSync(join(dir, "package.json"), "utf8")).name !== name) dir = dirname(dir);
  return dir;
};
const copy = (from, dir, files) => {
  const to = fileURLToPath(new URL(`../public/${dir}/`, import.meta.url));
  mkdirSync(to, { recursive: true });
  for (const f of files) cpSync(join(from, f), join(to, f));
};
copy(join(packageDir(require, "@mkljczk/bergamot-translator"), "worker"), "bergamot", [
  "translator-worker.js",
  "bergamot-translator-worker.js",
  "bergamot-translator-worker.wasm",
]);
// The version transformers.js depends on (see lib/translate/llm.ts).
const transformers = packageDir(require, "@huggingface/transformers");
const ort = packageDir(createRequire(join(transformers, "package.json")), "onnxruntime-web");
copy(join(ort, "dist"), "ort", ["ort-wasm-simd-threaded.asyncify.mjs", "ort-wasm-simd-threaded.asyncify.wasm"]);
