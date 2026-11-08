// Writes public/emoji-data.json for the emoji picker: every emoji up to
// the Twemoji version we ship (see twemoji.mjs), in order, compact:
// { groups: [{name, icon}], emoji: [[emoji, name, keywords, group, skins?]] }.
// Runs before builds; the picker loads it only when it opens.
// Pure data conversion; emojidata-cli.mjs writes the build asset.

export const MAX_VERSION = 17; // Twemoji 17

// emojibase groups (2: skin tone and hair components, not shown).
const GROUPS = [
  { name: "Smileys & People", icon: "😀", from: [0, 1] },
  { name: "Animals & Nature", icon: "🐻", from: [3] },
  { name: "Food & Drink", icon: "🍔", from: [4] },
  { name: "Travel & Places", icon: "🚗", from: [5] },
  { name: "Activities", icon: "⚽", from: [6] },
  { name: "Objects", icon: "💡", from: [7] },
  { name: "Symbols", icon: "❤️", from: [8] },
  { name: "Flags", icon: "🏁", from: [9] },
];

// The five one-tone variants, or none (emoji with two people mix tones).
const skinTones = (skins = []) => {
  const one = [1, 2, 3, 4, 5].map((t) => skins.find((s) => s.tone === t || (Array.isArray(s.tone) && s.tone.every((x) => x === t))));
  return one.every(Boolean) ? one.map((s) => s.emoji) : null;
};

export const compact = (data, shortcodes, maxVersion = MAX_VERSION) => {
  const groupOf = new Map(GROUPS.flatMap((g, i) => g.from.map((f) => [f, i])));
  const emoji = data
    .filter((e) => groupOf.has(e.group) && e.version <= maxVersion)
    .sort((a, b) => a.order - b.order)
    .map((e) => {
      const keywords = [...new Set([...(e.tags ?? []), ...[shortcodes[e.hexcode] ?? []].flat()])].join(" ");
      const skins = skinTones(e.skins?.filter((s) => !(s.version > maxVersion)));
      const row = [e.emoji, e.label, keywords, groupOf.get(e.group)];
      return skins ? [...row, skins] : row;
    });
  return { groups: GROUPS.map(({ name, icon }) => ({ name, icon })), emoji };
};
