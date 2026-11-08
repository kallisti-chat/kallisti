# Several servers: scope blocks and user audio preferences

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Blocks and audio preferences use only a user name globally. Bob on one server affects an unrelated Bob on another, including push filtering, quotes, search, and playback.

Priority: Medium.

Evidence: Source-confirmed name-only global storage and consumers.

## Requirements

- Bind preference access to a server once in Client and pass the block policy to child views.
- Scope storage by the real Kallisti origin or plain-Mumble endpoint, not a local bridge address.
- Define a legacy migration rule; do not copy ambiguous old entries to every server.

## Acceptance Criteria

- Two same-name users on separate servers have independent block, mute, and volume values.
- Quotes, message groups, mentions, search, pins, push payloads, persistence, and migration use the same scope.

## Notes

- apps/web/src/lib/settings.svelte.ts:21-29,78-86
- apps/web/src/components/Client.svelte:560,863,2035
- apps/web/src/components/ChatPane.svelte:355,1766

## Result (2026-11-08)

Client now binds one block/audio policy to the public Kallisti origin or real Mumble endpoint. Quotes, groups, mentions, search, pins, push payloads, and playback share that policy. Versioned storage keeps servers separate. Legacy name-only keys remain untouched but inactive because their server is unknown; users must set those preferences again.

Verification: Nine headless tests cover independent same-name users, real transport scope, reactivity, persistence, storage events, migration, and consumer policy. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.
