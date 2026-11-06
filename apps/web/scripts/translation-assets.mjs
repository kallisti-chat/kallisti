// Copies Bergamot's worker and WebAssembly (message translation, see
// lib/translate) to public/bergamot/: the worker loads them by their names,
// next to itself. Runs before builds; the page loads them only on the
// first translation. (The ONNX runtime of the larger model is bundled,
// see llm.ts.)
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
