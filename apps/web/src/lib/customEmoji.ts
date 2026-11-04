import { getContext, setContext } from "svelte";
import { emojiParts } from "./emoji";
import type { EmojiEntry } from "./emojiPicker";
import { t } from "./i18n.svelte";

// Custom server emoji: a name and an image on the server. In messages and
// as reactions they are ":name:"; Mumble clients see that text.
// url: relative to the server as it comes, absolute in a CustomEmojiSet.
export type CustomEmoji = { name: string; url: string };
// Personal emoji (see lib/personalEmoji) are "<:name:id>", the id of their
// image: mine are ours (with absolute URLs); personalUrl is any image's
// (null: a moderator removed it).
export type CustomEmojiSet = {
  list: CustomEmoji[];
  urls: ReadonlyMap<string, string>;
  mine: (CustomEmoji & { id: number })[];
  personalUrl: (id: number) => string | null;
};
// What message rendering needs: server emoji by name, personal ones by id.
export type EmojiImages = Pick<CustomEmojiSet, "urls"> & Partial<Pick<CustomEmojiSet, "personalUrl">>;

// As the server checks them (server/internal/emoji ValidName), and its
// limits (store.MaxEmoji, files.MaxEmojiBytes, the inline image types).
export const CUSTOM_NAME = "[a-z0-9_]{2,32}";
const NAME = new RegExp(`^${CUSTOM_NAME}$`);
const IN_COLONS = new RegExp(`^:(${CUSTOM_NAME}):$`);
// As the server's (server/internal/emoji Ref).
export const PERSONAL_REF = `<:(${CUSTOM_NAME}):([1-9]\\d{0,17})>`;
const REF = new RegExp(`^${PERSONAL_REF}$`);
const ANY_REF = new RegExp(PERSONAL_REF, "g");
// Either kind in text: a reference (name, id) or a server emoji (name).
const ANY_CUSTOM = new RegExp(`${PERSONAL_REF}|:(${CUSTOM_NAME}):`, "g");
const MAX_EMOJI = 200;
const MAX_BYTES = 256 * 1024;
export const EMOJI_TYPES = ["image/png", "image/gif", "image/webp", "image/jpeg"];

// The images must be on the server: anything else could track readers.
const isEmoji = (v: unknown): v is CustomEmoji =>
  !!v &&
  typeof v === "object" &&
  NAME.test((v as CustomEmoji).name) &&
  typeof (v as CustomEmoji).url === "string" &&
  /^\/api\/emoji\/\d+$/.test((v as CustomEmoji).url);

// A broken entry is left out (null: no list at all).
export const parseCustomEmoji = (v: unknown): CustomEmoji[] | null => (Array.isArray(v) ? v.filter(isEmoji) : null);

export const personalRef = (name: string, id: number) => `<:${name}:${id}>`;

export const parseRef = (emoji: string): { name: string; id: number } | null => {
  const m = REF.exec(emoji);
  return m ? { name: m[1]!, id: Number(m[2]) } : null;
};

// Plain text (as Mumble clients see it): personal emoji as ":name:".
export const refsAsNames = (text: string) => text.replace(ANY_REF, ":$1:");

export const customEmojiSet = (
  list: readonly CustomEmoji[],
  base: string,
  mine: readonly { name: string; id: number }[] = [],
  removed: readonly number[] = [],
): CustomEmojiSet => {
  const abs = list.map((e) => ({ name: e.name, url: base + e.url }));
  const gone = new Set(removed);
  const personalUrl = (id: number) => (gone.has(id) ? null : `${base}/api/emoji/personal/${id}`);
  return {
    list: abs,
    urls: new Map(abs.map((e) => [e.name, e.url])),
    mine: mine.flatMap((e) => (gone.has(e.id) ? [] : [{ ...e, url: personalUrl(e.id)! }])),
    personalUrl,
  };
};

export const customName = (emoji: string): string | null => {
  const m = IN_COLONS.exec(emoji);
  return m ? m[1]! : null;
};

// Unicode emoji (the server checks them), a custom one of this server
// (recent emoji may be from another server, or deleted), or one of ours.
export const knownEmoji = (emoji: string, set: CustomEmojiSet) => {
  const ref = parseRef(emoji);
  if (ref) return set.mine.some((e) => e.id === ref.id);
  const name = customName(emoji);
  return name === null || set.urls.has(name);
};

// The image of ":name:" or "<:name:id>" (null: text), and its name.
export const customImageOf = (emoji: string, images: EmojiImages): { name: string; url: string } | null => {
  const ref = parseRef(emoji);
  if (ref) {
    const url = images.personalUrl?.(ref.id);
    return url ? { name: ref.name, url } : null;
  }
  const name = customName(emoji);
  const url = name === null ? undefined : images.urls.get(name);
  return url ? { name: name!, url } : null;
};

// data-emoji: what the image is (for "Add to my emoji").
const customImage = (emoji: string, name: string, url: string) => {
  const img = document.createElement("img");
  img.className = "emoji custom";
  img.draggable = false;
  img.alt = img.title = `:${name}:`;
  img.src = url;
  img.dataset.emoji = emoji;
  return img;
};

// Replaces known ":name:" and "<:name:id>" in a (sanitized) DOM's text
// with images; not in code or links. Attributes are never touched.
// Removed personal emoji become ":name:" text.
export const addCustomEmojiImages = (root: Node, images: EmojiImages) => {
  if (images.urls.size === 0 && !images.personalUrl) return;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const texts: Text[] = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    if (!n.parentElement?.closest("code, pre, a")) texts.push(n as Text);
  }
  for (const node of texts) {
    const found = [...node.data.matchAll(ANY_CUSTOM)];
    if (found.length === 0) continue;
    const parts: (string | HTMLImageElement)[] = [];
    let last = 0;
    for (const m of found) {
      const image = customImageOf(m[0], images);
      parts.push(node.data.slice(last, m.index), image ? customImage(m[0], image.name, image.url) : m[3] ? m[0] : `:${m[1]}:`);
      last = m.index + m[0].length;
    }
    node.replaceWith(...parts, node.data.slice(last));
  }
};

// For the picker and the ":" suggestions (searched by name).
export const customEntries = (list: readonly CustomEmoji[]): EmojiEntry[] =>
  list.map((e) => ({ emoji: `:${e.name}:`, name: e.name, keywords: "", group: -1 }));

// Why a new emoji cannot be added (null: it can); the server checks again.
export const emojiFormError = (name: string, file: { size: number; type: string } | null, list: readonly CustomEmoji[]): string | null => {
  if (name.length < 2 || name.length > 32) return t("media.emojiNameLength");
  if (!NAME.test(name)) return t("media.emojiNameLetters");
  if (list.some((e) => e.name === name)) return t("media.emojiNameTaken");
  if (list.length >= MAX_EMOJI) return t("media.serverEmojiFull", { max: MAX_EMOJI });
  if (!file) return t("media.chooseImage");
  if (!EMOJI_TYPES.includes(file.type)) return t("media.emojiImageType");
  if (file.size > MAX_BYTES) return t("media.emojiImageSize");
  return null;
};

// A name from a file name: "Party Parrot.gif" -> "party_parrot".
export const emojiNameFromFile = (file: string) =>
  file
    .replace(/\.[^.]*$/, "")
    .toLowerCase()
    .replace(/[^a-z0-9_]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 32);

// Components below the client get the current server's emoji (as avatars
// do, see avatar.ts).
const KEY = Symbol("customEmoji");
const NONE: CustomEmojiSet = { list: [], urls: new Map(), mine: [], personalUrl: () => null };
export const setCustomEmoji = (get: () => CustomEmojiSet) => setContext(KEY, get);
export const customEmojiFor = (): (() => CustomEmojiSet) => getContext<(() => CustomEmojiSet) | undefined>(KEY) ?? (() => NONE);

// Only emoji (Unicode or known custom ones; spaces between them), at most
// 27: the message shows them large, as Discord does.
export const jumboEmoji = (text: string, images: EmojiImages) => {
  let count = 0;
  const rest = text.replace(ANY_CUSTOM, (all) => (customImageOf(all, images) ? (count++, " ") : all));
  for (const part of emojiParts(rest, "")) {
    if ("emoji" in part) count++;
    else if (part.text.trim()) return false;
  }
  return count > 0 && count <= 27;
};
