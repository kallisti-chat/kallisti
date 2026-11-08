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
