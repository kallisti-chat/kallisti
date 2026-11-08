// Dedicated build entry: no argv/file-URL comparison, so spaces and
// Windows path spelling cannot silently skip the required asset.
import { mkdirSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";
import { compact } from "./emojidata.mjs";

const require = createRequire(import.meta.url);
const data = require("emojibase-data/en/data.json");
const shortcodes = require("emojibase-data/en/shortcodes/github.json");
const publicDir = join(import.meta.dirname, "..", "public");
mkdirSync(publicDir, { recursive: true });
writeFileSync(join(publicDir, "emoji-data.json"), JSON.stringify(compact(data, shortcodes)));
