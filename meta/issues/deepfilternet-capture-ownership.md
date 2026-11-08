# Audio: isolate ownership of the shared DeepFilterNet node

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

A stopped capture keeps its reference to the shared DeepFilterNet node. Closing it or changing its filter disconnects every destination, including another server's active capture, and clears the active handlers.

Priority: Medium.

Evidence: Reproduced with fake media and two captures on one AudioContext.

## Requirements

- Release only the current capture's connection and handlers.
- Keep the cached node: the package dispose path leaks its allocation.
- Do not open a real microphone in tests.

## Acceptance Criteria

- With A stopped and B active on the same context, closing A or changing A's filter does not disconnect B or clear B's handlers.
- Normal stop, restart, close, and filter fallback remain correct.

## Notes

- apps/web/src/lib/voice/capture.svelte.ts:191,203,350,399

## Result (2026-11-08)

Each capture now disconnects only its own destination and clears shared handlers only while it owns them. The cached DeepFilterNet node is retained.

Verification: Fake-media regressions failed before the fix and now pass for stopped A/active B, close and filter changes, overload/error delivery, fallback, restart, and cache reuse. Full workspace tests/type checks and the web build passed. Independent reviews: 6.1-sol and Astra; no blockers.
