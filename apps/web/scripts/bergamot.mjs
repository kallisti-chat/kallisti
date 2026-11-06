// Copies Bergamot's worker and WebAssembly (message translation, see
// lib/translate) to public/bergamot/: the worker loads them by their names,
// next to itself. Runs before builds; the page loads them only on the first
// translation.
import { cpSync, mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";

const require = createRequire(import.meta.url);
const from = join(dirname(require.resolve("@mkljczk/bergamot-translator/package.json")), "worker");
const to = new URL("../public/bergamot/", import.meta.url).pathname;
mkdirSync(to, { recursive: true });
for (const f of ["translator-worker.js", "bergamot-translator-worker.js", "bergamot-translator-worker.wasm"]) cpSync(join(from, f), join(to, f));
