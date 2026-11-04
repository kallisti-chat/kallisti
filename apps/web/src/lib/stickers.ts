import { CUSTOM_NAME, emojiNameFromFile } from "./customEmoji";
import { t } from "./i18n.svelte";

// Stickers: large pictures sent as a message of their own (see the server's
// sidecar/stickers.go). A sticker message carries the sticker's id and
// name; one whose id is not in the list was deleted (its name shows).
// url: relative to the server.
export type Sticker = { id: number; name: string; description?: string; emoji?: string; pack?: string; url: string };
export type MessageSticker = { id: number; name: string };
export type StickerFields = { name: string; description: string; emoji: string; pack: string };

// As the server checks them (sidecar stickerFields, store.MaxStickers,
// files.MaxStickerBytes).
const NAME = new RegExp(`^${CUSTOM_NAME}$`);
export const MAX_STICKERS = 200;
export const MAX_STICKER_BYTES = 512 * 1024;
// Stickers are shown at most 160 px: twice that for retina screens.
export const STICKER_SIZE = 320;

const isStr = (v: unknown): v is string => typeof v === "string";

// The images must be on the server: anything else could track readers.
const isSticker = (v: unknown): v is Sticker => {
  if (!v || typeof v !== "object") return false;
  const s = v as Sticker;
  return (
    Number.isSafeInteger(s.id) &&
    s.id > 0 &&
    isStr(s.name) &&
    NAME.test(s.name) &&
    s.url === `/api/stickers/${s.id}` &&
    [s.description, s.emoji, s.pack].every((f) => f === undefined || isStr(f))
  );
};

// A broken entry is left out (null: no list at all).
export const parseStickers = (v: unknown): Sticker[] | null => (Array.isArray(v) ? v.filter(isSticker) : null);

export const isMessageSticker = (v: unknown): v is MessageSticker =>
  !!v && typeof v === "object" && Number.isSafeInteger((v as MessageSticker).id) && isStr((v as MessageSticker).name);

// What places that show only text show (as the server stores it).
export const stickerText = (name: string) => `[sticker: ${name}]`;

// The picker's sections: stickers without a pack first, then by pack, each
// in the list's order (the server sorts by pack and name).
export const stickerGroups = (list: readonly Sticker[]): { pack: string; stickers: Sticker[] }[] => {
  const groups = new Map<string, Sticker[]>();
  for (const s of [...list].sort((a, b) => Number(!!a.pack) - Number(!!b.pack))) {
    const key = s.pack ?? "";
    groups.set(key, [...(groups.get(key) ?? []), s]);
  }
  return [...groups].map(([pack, stickers]) => ({ pack, stickers }));
};

export const stickerSearch = (list: readonly Sticker[], query: string): readonly Sticker[] => {
  const q = query.trim().toLowerCase();
  if (!q) return list;
  return list.filter((s) => [s.name, s.description, s.pack, s.emoji].some((f) => f?.toLowerCase().includes(q)));
};

const EMOJI = /^(?:\p{Extended_Pictographic}|\p{Regional_Indicator}|\p{Emoji_Component}|‍|️)+$/u;

// Why the fields cannot be saved (null: they can); self: the sticker
// being renamed.
export const stickerForm = (f: StickerFields, list: readonly Sticker[], self?: number): string | null => {
  if (!NAME.test(f.name)) return t("media.stickerNameRule");
  if (list.some((s) => s.name === f.name && s.id !== self)) return t("media.stickerNameTaken");
  if ([...f.description].length > 100) return t("media.stickerDescriptionLength");
  if ([...f.pack].length > 32) return t("media.stickerPackLength");
  if (f.emoji && !EMOJI.test(f.emoji)) return t("media.stickerEmojiInvalid");
  return null;
};

// Requests (who may manage emoji; the server checks): a token to upload a
// new sticker with these fields, rename (the fields again), delete.
export const stickerTokenRequest = (f: StickerFields) => JSON.stringify({ type: "sticker-token", ...f });
export const renameStickerRequest = (sticker: number, f: StickerFields) => JSON.stringify({ type: "rename-sticker", sticker, ...f });
export const deleteStickerRequest = (sticker: number) => JSON.stringify({ type: "delete-sticker", sticker });

// Names for many files added at once: each file's name made a valid one,
// with _2, _3, ... where it is taken (by the list or an earlier file) or
// too short.
export const bulkNames = (files: readonly string[], list: readonly Sticker[]): string[] => {
  const taken = new Set(list.map((s) => s.name));
  return files.map((f) => {
    const base = emojiNameFromFile(f) || "sticker";
    let name = base;
    for (let n = 2; !NAME.test(name) || taken.has(name); n++) name = `${base.slice(0, 28)}_${n}`;
    taken.add(name);
    return name;
  });
};
