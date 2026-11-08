# Threads: delete large histories without exceeding SQLite limits

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Thread deletion loads all messages and constructs a statement with one bind parameter per message. Large histories exceed SQLite's variable limit.

Priority: Medium.

Evidence: Deleting a thread with 32,767 messages failed with too many SQL variables.

## Requirements

- Delete by channel or use bounded batches within the same transaction.
- Preserve attachment cleanup and transactional integrity; avoid loading full message bodies only for deletion.

## Acceptance Criteria

- A thread with at least 32,767 messages deletes successfully.
- Files and related message data are cleaned up, and rollback behavior remains covered.

## Notes

- server/internal/store/threads.go:208
- server/internal/store/store.go:1186,1253

## Result (2026-11-08)

Thread deletion uses channel/subqueries within one transaction instead of one bind variable per message. It loads only attachment keys, not full message bodies. Related data cleanup and the retained hidden channel remain unchanged.

Verification: The 32,767-message regression failed with too many SQL variables before the fix and now passes with attachment, poll/vote, reaction, follow, and read-mark cleanup. A forced final-step failure rolls back the earlier deletes; retry succeeds. Full Go race tests and vet passed. Independent 6.1-sol review approved the fix.
