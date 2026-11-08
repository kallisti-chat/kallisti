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
