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
