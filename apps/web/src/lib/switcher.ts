// The quick switcher (Ctrl/Cmd+K) and Alt+Up/Down between chats.

// Names that start with the query first, then names with a word that
// starts with it, then names that have it, then names with its letters in
// order ("lo2" finds "Lounge 2"). Any case.
const rank = (name: string, q: string): number | null => {
  const n = name.toLowerCase();
  if (n.startsWith(q)) return 0;
  if (n.split(/[\s_-]+/).some((w) => w.startsWith(q))) return 1;
  if (n.includes(q)) return 2;
  let at = 0;
  for (const c of n) if (c === q[at]) at++;
  return at === q.length ? 3 : null;
};

export const matchPlaces = <T extends { name: string }>(places: readonly T[], query: string): T[] => {
  const q = query.trim().toLowerCase();
  if (!q) return [...places];
  return places
    .flatMap((p) => {
      const r = rank(p.name, q);
      return r === null ? [] : [{ p, r }];
    })
    .sort((a, b) => a.r - b.r) // stable: the list order within a rank
    .map(({ p }) => p);
};

// The place above (-1) or below (1) the current one, around the ends;
// only: the places to stop at (unread ones). null: none.
export const stepPlace = (keys: readonly string[], current: string | null, step: 1 | -1, only: (key: string) => boolean = () => true) => {
  const at = current === null ? -1 : keys.indexOf(current);
  const from = at >= 0 ? at : step === 1 ? -1 : keys.length;
  for (let i = 1; i <= keys.length; i++) {
    const key = keys[(((from + step * i) % keys.length) + keys.length) % keys.length]!;
    if (key !== current && only(key)) return key;
  }
  return null;
};

export const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
// The modifier key's name, for the shortcut lists.
export const MOD = IS_MAC ? "⌘" : "Ctrl";

export type NavAction = "switcher" | "shortcuts" | "settings" | { step: 1 | -1; unread: boolean } | null;

// What a key press does: Cmd (Mac) or Ctrl + K or /, Alt(+Shift)+Up/Down.
// Not for keys that something else used (the message box: Up edits the
// last message) or while an IME composes. K by its key position too, for
// other keyboard layouts.
export const navAction = (e: KeyboardEvent, mac: boolean): NavAction => {
  if (e.defaultPrevented || e.isComposing) return null;
  if (mac && e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && e.key === ",") return "settings";
  const mod = (mac ? e.metaKey : e.ctrlKey) && !e.altKey;
  const k = /^[a-z]$/i.test(e.key) ? e.key.toLowerCase() : e.code === "KeyK" ? "k" : e.key;
  if (mod && k === "k") return "switcher";
  if (mod && e.key === "/") return "shortcuts";
  if (e.altKey && !e.ctrlKey && !e.metaKey && (e.key === "ArrowUp" || e.key === "ArrowDown"))
    return { step: e.key === "ArrowDown" ? 1 : -1, unread: e.shiftKey };
  return null;
};
