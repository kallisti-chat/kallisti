# Localization

## Summary

Users asked for Kallisti in their languages. The client (web, desktop,
Android: one code base) gets English, German, French, Spanish, Japanese,
Simplified and Traditional Chinese. The browser's language is the default;
Settings has a language choice (per device). Translations are machine-made
first and reviewed by native speakers in the PR.

Our own small module (no dependency): English dictionaries in TypeScript
are the source and the key type; other languages fall back to English per
key; plurals with Intl.PluralRules; other languages load on demand.

Not in this issue: texts the server sends (refusals, errors): they need
codes first (a follow-up), the website, the docs.

## Sub-issues

- Core module, language choice and setting
- Move the client's texts into dictionaries (by area)
- Translations (de, fr, es, ja, zh-Hans, zh-Hant)

## Acceptance Criteria

- Every visible text of the client comes from the dictionaries.
- The language follows the browser, and the setting overrides it.
- All 7 languages complete; a missing key shows English.
