# Messages: preserve literal emoji references when editing

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Personal emoji toRefs excludes code and URLs, but fromRefs does not. Opening and saving an unchanged edit can alter literal message content.

Priority: Medium.

Evidence: An unchanged edit converted literal `<:blob:7>` inside code to `:blob:`.

## Requirements

- Use matching exclusions for both directions while retaining normal personal-emoji editing.

## Acceptance Criteria

- Unchanged edits preserve literal references in inline code, fenced code, and URLs.
- Normal editable emoji aliases still round-trip.

## Notes

- apps/web/src/lib/personalEmoji.ts:31-56

## Result (2026-11-08)

Both edit conversion directions now share the same code and URL exclusions. Literal references remain unchanged while normal personal aliases still round-trip.

Verification: Four literal regressions failed before the fix and now pass for inline code, fenced code, and HTTP/HTTPS URLs. Normal aliases and server-name collisions also pass. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.
