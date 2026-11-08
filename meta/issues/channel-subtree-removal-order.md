# Channels: remove descendants before their parents

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

`State.Subtree` promises children-first order. It recursively collects each
child's subtree, then sorts the flattened descendants by channel ID. That sort
can place a parent before its own child. For a chain 10 -> 20 -> 30, the result
can be `[20, 30, 10]` instead of a child-first order.

`RemoveChannel` returns this order for removal broadcasts. The recent access
cleanup fix deliberately preserved it.

Evidence: source finding from the channel-deletion review. Client impact has not
been tested; no crash or permission bypass is claimed.

## Requirements

- Keep every descendant before its parent in subtree removal order.
- Keep ordering deterministic, including between sibling subtrees, without
  sorting the flattened postorder result.
- Preserve affected-user moves, listener cleanup, visibility reconciliation,
  suppression, and exactly one move callback per affected user.

## Acceptance Criteria

- State tests cover at least three levels and sibling branches, with channel IDs
  that do not encode tree depth. Every child precedes its parent and each
  channel appears once in a deterministic result.
- A server test verifies child-first channel-removal broadcasts while preserving
  the existing move-before-removal sequence.
- Channel deletion/access regressions and relevant Mumble race tests pass.

## Notes

- `server/internal/mumbled/state.go`: `Subtree` and `RemoveChannel`.
- `server/internal/mumbled/messages.go`: `removeChannelLocked`.
- Related completed fix: [Channel-deletion access cleanup](channel-deletion-access-cleanup.md).

## Result (2026-11-08)

Subtree traversal now sorts direct siblings before recursion instead of sorting flattened descendants. Removal order is deterministic and keeps every child before its parent.

Verification: Regressions failed before the fix and now cover three levels, sibling branches, and IDs unrelated to depth. Server tests preserve move-before-removal broadcasts, suppression, listener/link cleanup, surviving visibility, and exactly one callback per moved user. Existing deletion/access regressions, full Go race tests, and vet passed. Independent reviews: 6.1-sol and Astra; no blockers.
