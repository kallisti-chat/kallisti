# Streams: preserve both decoded audio channels

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Stream playback destructures only the first decoded plane and makes a mono buffer. Sound that exists only in the right channel is lost.

Priority: Medium.

Evidence: Reproduced with a fake audio context and right-only stereo samples.

## Requirements

- Preserve stereo planes, or deliberately downmix all planes instead of taking only the left plane.
- Keep current playout timing, volume, deafen, and suspended-context behavior.

## Acceptance Criteria

- A right-only stereo fixture produces nonzero output; a stereo-preserving implementation copies both channels.
- Mono and existing stream-sound tests pass without real audio devices.

## Notes

- apps/web/src/lib/stream/client.svelte.ts:454-466

## Result (2026-11-08)

Stream playback now creates a buffer for every decoded plane and copies each plane to its matching channel. Timing, gain, deafen, and suspended-context handling are unchanged.

Verification: The right-only stereo regression failed before the fix and now passes alongside mono and existing stream-sound tests, without real audio devices. Full workspace tests/type checks and the web build passed. Independent reviews: 6.1-sol and Astra; no blockers.
