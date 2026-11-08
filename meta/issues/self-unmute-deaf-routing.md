# Voice: refresh routing after implicit self-undeafen

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

SelfMute:false can clear SelfDeaf implicitly, but routing invalidation only checks whether SelfDeaf was present in the incoming message. The cached receiver set can keep the user deaf.

Priority: Medium.

Evidence: Reproduced with a local network regression test.

## Requirements

- Invalidate routing when the effective deaf state changes, including implicit changes.

## Acceptance Criteria

- A SelfMute-only unmute after self-deafening restores audio delivery.
- Explicit mute/deaf combinations and existing routing tests pass.

## Notes

- server/internal/mumbled/messages.go:325
- server/internal/mumbled/targets.go:136
