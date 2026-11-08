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
