import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
// @ts-expect-error: a build script without types
import { compact } from "../scripts/emojidata.mjs";

const tones = (base: string, label: string) =>
  [1, 2, 3, 4, 5].map((tone) => ({ emoji: base + String.fromCodePoint(0x1f3fa + tone), label: `${label}: tone ${tone}`, tone }));

describe("compact emoji data", () => {
  const data = [
    { emoji: "👍", label: "thumbs up", tags: ["+1", "thumb"], group: 1, order: 2, version: 0.6, skins: tones("👍", "thumbs up") },
    { emoji: "😀", label: "grinning face", tags: ["face"], group: 0, order: 1, version: 1 },
    { emoji: "🏻", label: "light skin tone", group: 2, order: 3, version: 1 },
    { emoji: "🫝", label: "pickle", group: 4, order: 4, version: 18 },
    { emoji: "🍔", label: "hamburger", group: 4, order: 5, version: 1 },
    { emoji: "🇦", label: "regional indicator A", order: 0, version: 0.6 },
  ];
  const shortcodes = { "1F44D": ["+1", "thumbsup"], "1F600": "grinning" };
  const hex = (e: string) => [...e].map((c) => c.codePointAt(0)!.toString(16).toUpperCase()).join("-");
  const out = compact(
    data.map((e) => ({ ...e, hexcode: hex(e.emoji) })),
    shortcodes,
    17,
  );

  it("keeps emoji up to the Twemoji version, in order, without components", () => {
    expect(out.emoji.map((e: unknown[]) => e[0])).toEqual(["😀", "👍", "🍔"]);
  });

  it("merges smileys and people into one group", () => {
    expect(out.groups.map((g: { name: string }) => g.name)[0]).toBe("Smileys & People");
    const group = (e: string) => out.emoji.find((x: unknown[]) => x[0] === e)[3];
    expect(group("😀")).toBe(group("👍"));
    expect(out.groups[group("🍔")].name).toBe("Food & Drink");
  });

  it("keeps names, keywords with shortcodes, and the five skin tones", () => {
    expect(out.emoji[1]).toEqual(["👍", "thumbs up", "+1 thumb thumbsup", 0, tones("👍", "").map((t) => t.emoji)]);
    expect(out.emoji[0]).toEqual(["😀", "grinning face", "face grinning", 0]);
  });
});

describe("emoji-data build entry", () => {
  let root: string;
  let entry: string;
  const web = resolve(import.meta.dirname, "..");
  const output = () => join(root, "public", "emoji-data.json");
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "kallisti emoji data "));
    mkdirSync(join(root, "scripts"));
    for (const name of readdirSync(join(web, "scripts")).filter((n) => /^emojidata.*\.mjs$/.test(n))) {
      copyFileSync(join(web, "scripts", name), join(root, "scripts", name));
    }
    symlinkSync(join(web, "node_modules"), join(root, "node_modules"), "junction");
    const scripts = JSON.parse(readFileSync(join(web, "package.json"), "utf8")).scripts;
    const script = scripts.build.match(/node (scripts\/emojidata[^ ]*\.mjs)/)![1];
    expect(scripts.dev).toContain(`node ${script}`);
    entry = join(root, script);
  });
  afterEach(() => rmSync(root, { recursive: true, force: true }));

  it("generates required data from a checkout with spaces, outside its cwd", () => {
    const run = spawnSync(process.execPath, [entry], { cwd: tmpdir(), encoding: "utf8" });
    expect(run.status, run.stderr).toBe(0);
    const data = JSON.parse(readFileSync(output(), "utf8"));
    expect(data.emoji.length).toBeGreaterThan(1000);
    expect(data.groups.length).toBe(8);
  });

  it("does not depend on the platform's argv path spelling", () => {
    const code = `process.argv[1] = ${JSON.stringify("C:\\Users\\Alice Example\\kallisti\\scripts\\emojidata.mjs")}; await import(${JSON.stringify(pathToFileURL(entry).href)});`;
    const run = spawnSync(process.execPath, ["--input-type=module", "--eval", code], { encoding: "utf8" });
    expect(run.status, run.stderr).toBe(0);
    expect(existsSync(output())).toBe(true);
  });

  it("imports the data helpers without generating assets", () => {
    const url = pathToFileURL(join(root, "scripts", "emojidata.mjs")).href;
    const run = spawnSync(process.execPath, ["--input-type=module", "--eval", `await import(${JSON.stringify(url)})`], { encoding: "utf8" });
    expect(run.status, run.stderr).toBe(0);
    expect(existsSync(output())).toBe(false);
  });

  it("fails the build entry if required data cannot be written", () => {
    writeFileSync(join(root, "public"), "not a directory");
    const run = spawnSync(process.execPath, [entry], { encoding: "utf8" });
    expect(run.status).not.toBe(0);
    expect(existsSync(output())).toBe(false);
  });
});
