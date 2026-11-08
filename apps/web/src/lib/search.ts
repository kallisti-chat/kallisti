import { quoted } from "./suggest";

// "from:name", "in:#text", "in:@user", and "in:voice:name"; names with
// spaces can be quoted. Bare "in:" names keep the supplied place order
// (Client supplies text, then voice, then direct chats).
export type SearchQuery = { text: string; author?: string; channel?: number };
export type SearchPlace = { name: string; kind: "text" | "voice" | "dm"; id: number };

export const searchPlaceFilter = (place: SearchPlace) =>
  `in:${place.kind === "text" ? "#" : place.kind === "dm" ? "@" : "voice:"}${quoted(place.name)}`;

const FILTER = /(^|\s)(from|in):([#@]|voice:)?(?:"([^"]+)"|(\S+))/gi;

// An unknown chat is -1: the server finds nothing there. Explicit kinds
// never fall back to another namespace with the same name.
export const parseSearch = (query: string, places: readonly SearchPlace[]): SearchQuery => {
  const out: Omit<SearchQuery, "text"> = {};
  const text = query.replace(FILTER, (_, before: string, key: string, prefix: string | undefined, quoted?: string, word?: string) => {
    const value = quoted ?? word ?? "";
    if (key.toLowerCase() === "from") out.author = value;
    else {
      const kind = prefix === "#" ? "text" : prefix === "@" ? "dm" : prefix ? "voice" : undefined;
      out.channel = places.find((p) => (!kind || p.kind === kind) && p.name.toLowerCase() === value.toLowerCase())?.id ?? -1;
    }
    return before;
  });
  return { text: text.replace(/\s+/g, " ").trim(), ...out };
};
