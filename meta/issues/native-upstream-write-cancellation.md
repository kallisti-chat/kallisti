# Desktop bridge: cancel blocked upstream writes when the client leaves

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

The task that reads the local WebSocket also awaits upstream writes. If the upstream stops reading but remains open, the task cannot see local EOF. The two-second close timeout starts only after that task returns.

Priority: Medium.

Evidence: Source finding; native stalled-write behavior was not exercised.

## Requirements

- Bound upstream writes or separate close cancellation from the blocked write.
- Keep frame serialization and normal close-reason forwarding.

## Acceptance Criteria

- A fake upstream that stops reading cannot retain the relay indefinitely after the client closes.
- Normal bidirectional relay, large frames, and close reasons remain covered.

## Notes

- apps/desktop/src-tauri/src/bridge.rs:374-401,495-503

## Result (2026-11-08)

Complete binary, text, and close writes have a 10-second limit. Failure ends the relay without further writes after a partial frame. Serialization and normal close-reason forwarding remain intact.

Verification: Stalled-write regressions failed before the fix and now pass for local close/EOF and close writes. Tests preserve bidirectional 256 KiB frames, assembly, and server close reasons. A valid upstream slower than the whole-write limit can now be disconnected. Offline Rust tests passed (51 unit tests, one ignored; 13 integration tests, with real-Murmur tests gated off). Independent 6.1-sol and Astra reviews approved the fix.
