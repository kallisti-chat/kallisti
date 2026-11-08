# Message HTML: prevent styles from covering the application

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Message HTML can retain position:fixed, full-window bounds, and a very high z-index. An external Mumble message can keep the CSS needed to cover the application with a link or false prompt. Normal built-in stored chat escapes HTML.

Priority: Medium; external HTML-enabled Mumble servers.

Evidence: A sanitizer probe retained fixed positioning; full browser layout was not tested.

## Requirements

- Allow only the formatting styles that messages need, or remove inline styles.
- Preserve the existing remote-resource restrictions and safe links.

## Acceptance Criteria

- Tests reject fixed/absolute overlay positioning and excessive layout or stacking controls.
- Existing sanitizer, emoji, and safe-formatting tests pass.

## Notes

- apps/web/src/lib/sanitize.ts:17-35
- Related but separate: message-forms.md

## Result (2026-11-08)

Inline styles are limited to text colors, font family/weight/style, and decoration with default thickness. Positioning, sizing, stacking, and unresolved substitutions are rejected. Retained styles are serialized from CSSOM, not raw input. Existing resource and link restrictions remain.

Verification: layout probes were red before the correction. Sanitizer/custom/personal emoji tests (67) passed, as did full workspace tests/type checks and the production web build. Headless Chrome 150 preserved safe color/underline and rejected 24 plain/emoji attack cases, including large decoration shorthands, variable/environment/attribute fallbacks, and CSS if() substitution. Browser checks exposed the shorthand-longhand difference absent in jsdom. Independent 6.1-sol and Astra follow-up reviews found no remaining blockers.

The separate message-forms issue remains open.
