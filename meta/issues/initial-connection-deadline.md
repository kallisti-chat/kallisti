# Connections: bound the initial Mumble handshake

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

An initial WebSocket can open without ever reaching Mumble sync. The login remains on Connecting indefinitely while reconnect attempts have a 15-second deadline.

Priority: Medium.

Evidence: Source finding; reconnect has a deadline but initial start does not.

## Requirements

- Apply a common bounded handshake lifecycle to initial and reconnect attempts.
- Do not let an old timeout close a newer successful connection.

## Acceptance Criteria

- Fake-timer tests close or fail an initial connection that never syncs.
- Successful handshakes, reconnects, cancellation, and replacement by a new login remain correct.

## Notes

- apps/web/src/lib/serverSession.svelte.ts:137-151,223-240
