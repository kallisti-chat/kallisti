# Channel deletion: reconcile moved users and audio listeners

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Deleting a channel moves its users to the parent without the listener cleanup, permission-cache flush, and visibility reconciliation used by normal moves. A user can keep receiving audio after losing Listen permission.

Priority: High.

Evidence: Reproduced with a local network regression test.

## Requirements

- Share the post-move access cleanup between explicit moves and deletion-induced moves.
- Keep channel-removal ordering, suppression, and move callbacks unchanged. Do not call the normal move path after state has already moved the users.

## Acceptance Criteria

- A test with location-dependent Listen permission proves that deletion removes a now-forbidden subscription and stops audio delivery.
- Tests cover permission-cache invalidation, visibility, suppression, and one move callback per affected user.
- Relevant Mumble tests pass with the race detector.

## Notes

- server/internal/mumbled/messages.go:601
- server/internal/mumbled/perms.go:80
- server/internal/mumbled/targets.go:157

## Result (2026-11-08)

Explicit moves and deletion-induced moves now share listener cleanup, permission-cache invalidation, and visibility reconciliation after the move/removal broadcasts. Suppression and one callback per moved user are preserved.

Verification: failing buffered-outbox regressions now pass, including primed normal/targeted audio caches, permitted subscriptions, root/non-root destinations, visibility ordering, and suppression. `go test -race ./...` and `go vet ./...` passed. Independent reviews: 6.1-sol and Astra; no blockers.
