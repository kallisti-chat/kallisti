import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { expect, it } from "vitest";

const require = createRequire(import.meta.url);
const manifest = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));

it("does not declare an unused second compiler under a native alias", () => {
  expect(manifest.devDependencies).not.toHaveProperty("@typescript/native");
});

it("keeps the TypeScript compiler required by svelte-check", () => {
  const fromCheck = createRequire(require.resolve("svelte-check/package.json"));
  expect(fromCheck.resolve("typescript")).toBe(require.resolve("typescript"));
});
