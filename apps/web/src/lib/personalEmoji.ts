import { CUSTOM_NAME, customImageOf, emojiFormError, parseRef, personalRef, PERSONAL_REF, type CustomEmojiSet } from "./customEmoji";
import type { EmojiEntry } from "./emojiPicker";
import { t } from "./i18n.svelte";

// Personal emoji: registered users keep their own (server/internal/
// sidecar/useremoji.go). Messages and reactions refer to one as
// "<:name:id>", the id of its image, so everyone sees it. We type ":name:"
// and the reference goes out; server emoji keep their names.
export type PersonalEmoji = { name: string; id: number };

// As the server's limit (store.MaxUserEmoji).
export const MAX_PERSONAL = 50;

const NAME = new RegExp(`^${CUSTOM_NAME}$`);
const isPersonal = (v: unknown): v is PersonalEmoji =>
  !!v && typeof v === "object" && NAME.test((v as PersonalEmoji).name) && Number.isSafeInteger((v as PersonalEmoji).id) && (v as PersonalEmoji).id > 0;

export const parsePersonalEmoji = (v: unknown): PersonalEmoji[] | null => (Array.isArray(v) ? v.filter(isPersonal) : null);
export const parseRemovedEmoji = (v: unknown): number[] | null =>
  Array.isArray(v) ? v.filter((id): id is number => Number.isSafeInteger(id)) : null;

type Names = ReadonlyMap<string, unknown>; // the server emoji's names

// The same emoji: as written, or the same personal image.
export const sameEmoji = (a: string, b: string) => {
  if (a === b) return true;
  const ra = parseRef(a);
  return !!ra && ra.id === parseRef(b)?.id;
};

// Not in code or links: there ":name:" stays text.
const ASIDE = /```[\s\S]*?```|`[^`\n]+`|https?:\/\/\S+/g;
const outsideCode = (text: string, f: (part: string) => string) => {
  let out = "";
  let last = 0;
  for (const m of text.matchAll(ASIDE)) {
    out += f(text.slice(last, m.index)) + m[0];
    last = m.index + m[0].length;
  }
  return out + f(text.slice(last));
};

// What we send: our ":name:" as references (not server emoji names).
export const toRefs = (text: string, mine: readonly PersonalEmoji[], server: Names) =>
  outsideCode(text, (part) =>
    part.replace(new RegExp(`${PERSONAL_REF}|:(${CUSTOM_NAME}):`, "g"), (all, _r, _i, name?: string) => {
      const e = name && !server.has(name) ? mine.find((m) => m.name === name) : undefined;
      return e ? personalRef(e.name, e.id) : all;
    }),
  );

// What we edit: references that ":name:" would send again, as ":name:".
export const fromRefs = (text: string, mine: readonly PersonalEmoji[], server: Names) =>
  outsideCode(text, (part) =>
    part.replace(new RegExp(PERSONAL_REF, "g"), (all, name: string, id: string) =>
      !server.has(name) && mine.some((m) => m.name === name && m.id === Number(id)) ? `:${name}:` : all,
    ),
  );

// A picked emoji for the message box: one of ours as ":name:" when that
// sends it again.
export const composerEmoji = (emoji: string, mine: readonly PersonalEmoji[], server: Names) => (parseRef(emoji) ? fromRefs(emoji, mine, server) : emoji);

// For the picker and the ":" suggestions: our emoji, as references.
export const personalEntries = (mine: readonly PersonalEmoji[]): EmojiEntry[] =>
  mine.map((e) => ({ emoji: personalRef(e.name, e.id), name: e.name, keywords: "", group: -1 }));

// Why an emoji cannot be added (null: it can); the server checks again.
// upload: a new image (else one from a message).
export const personalFormError = (
  name: string,
  file: { size: number; type: string } | null,
  mine: readonly PersonalEmoji[],
  server: Names,
  upload = true,
): string | null => {
  if (mine.some((e) => e.name === name)) return t("media.personalNameTaken");
  if (server.has(name)) return t("media.personalNameOnServer");
  if (mine.length >= MAX_PERSONAL) return t("media.personalEmojiFull", { max: MAX_PERSONAL });
  return emojiFormError(name, upload ? file : { size: 0, type: "image/png" }, []);
};

// name, or with _2, _3, … when taken (at most 32 letters).
export const freeName = (name: string, taken: ReadonlySet<string>) => {
  let free = name;
  for (let n = 2; taken.has(free); n++) free = name.slice(0, 32 - `_${n}`.length) + `_${n}`;
  return free;
};

// What we may do with an emoji in a message or reaction (null: nothing,
// it shows no image): add it to ours under a free name (add, unless it is
// ours already), or remove a personal one everywhere (remove: moderators).
export const emojiActions = (emoji: string, set: CustomEmojiSet, rights: { add: boolean; remove: boolean }) => {
  const image = customImageOf(emoji, set);
  if (!image) return null;
  const ref = parseRef(emoji);
  const mine = !!ref && set.mine.some((e) => e.id === ref.id);
  const taken = new Set([...set.urls.keys(), ...set.mine.map((e) => e.name)]);
  return {
    ...image,
    kind: ref ? ("personal" as const) : ("server" as const),
    mine,
    add: rights.add && !mine ? freeName(image.name, taken) : null,
    remove: rights.remove && ref ? emoji : null,
  };
};
