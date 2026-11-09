import { createRequire } from "node:module";
import { expect, it } from "vitest";

const require = createRequire(import.meta.url);
const fromTransformers = createRequire(require.resolve("@huggingface/transformers"));

it.each(["onnxruntime-node", "sharp"])("does not install the unused %s backend for browser translation", (name) => {
  expect(() => fromTransformers.resolve(name)).toThrow(/Cannot find module/);
});

it("keeps the browser ONNX runtime available to Transformers.js", () => {
  expect(fromTransformers.resolve("onnxruntime-web")).toContain("onnxruntime-web");
});
