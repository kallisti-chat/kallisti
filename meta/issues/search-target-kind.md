# Search: distinguish DM and channel targets with the same name

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Search removes @ and # before lookup, and the merged name map favors text channels. A same-name DM, text channel, or voice channel can select the wrong target.

Priority: Medium.

Evidence: A targeted test resolved in:@general to text channel #general.

## Requirements

- Retain target kind during parsing and resolve within the correct namespace.

## Acceptance Criteria

- Same-name DM, text, and voice fixtures resolve correctly for supported explicit prefixes.
- Existing bare-name search behavior remains documented and tested.

## Notes

- apps/web/src/lib/search.ts:13
- apps/web/src/components/Client.svelte:1143

## Result (2026-11-08)

Search retains typed places instead of merging names. Explicit #text, @DM, and voice: filters resolve only in their namespace; suggestions use the same formatting. Bare names retain text, then voice, then DM priority.

Verification: Fourteen tests cover same-name targets, quoted prefixes, unknown targets without cross-kind fallback, bare priority, and suggestion round trips. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.
