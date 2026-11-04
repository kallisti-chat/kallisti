# Translations

The client's texts, one folder per language. `en` is the source: its keys
are the type that `t()` checks. Other languages may leave keys out; those
show in English.

- `lib/i18n.ts`: languages, plural forms, `around()`; `lib/i18n.svelte.ts`:
  the language in use and `t(key, values)`.
- Keys are `area.name`, one file per area (`common` is shared words). The
  name says what the text is for, not what it says.
- Values: `{name}` in the text, filled in by `t("chat.joined", { name })`.
  Keep the placeholders as they are; their order may change.
- Plurals: `{ one: "{count} message", other: "{count} messages" }`. Each
  language has its forms (Intl.PluralRules: zero, one, two, few, many,
  other); Japanese and Chinese have only `other`.
- A link or button inside a sentence: the sentence has `{link}` (or
  another name), and the code puts the element there (`around()`).

## For translators

Change only the texts on the right. Keep the tone short and plain, as
the English. "Kallisti" and "Mumble" stay as they are. Where the
English is unclear, the key and the code around it tell the context;
ask in the pull request.
