import type { Message } from "../lib/i18n";

// Another language's texts for an English area file: any of its keys,
// no others.
export type Translation<T> = { readonly [K in keyof T]?: Message };
