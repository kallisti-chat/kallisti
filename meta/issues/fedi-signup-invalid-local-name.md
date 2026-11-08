# Federation signup: offer a name when the remote handle is unusable locally

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

An automatic name from a valid remote handle can be reserved locally (for example superuser) or exceed local limits. The server returns 400, but the client only offers name selection for 409, so retry repeats the invalid name.

Priority: Medium.

Evidence: Source finding; signup response and client recovery disagree.

## Requirements

- Return a specific recoverable name-selection outcome or recognize it in the client.
- Do not weaken local name validation or repeat authentication unnecessarily.

## Acceptance Criteria

- Reserved and overlong automatic remote names open local-name selection and can finish with a valid choice.
- Ordinary signup, name conflicts, and unrelated 400 errors retain their intended behavior.

## Notes

- server/internal/auth/fedi.go:656-665
- server/internal/auth/auth.go:560
- server/internal/names/names.go:47
- apps/web/src/components/Login.svelte:278-298,355-366

## Result (2026-11-08)

An unusable automatic remote name now returns the existing recoverable 409 name-selection outcome. Explicit invalid local choices and unrelated bad requests remain 400. The verified remote signup stays pending until a valid local choice succeeds.

Verification: Reserved/overlong HTTP regressions failed before the fix and now pass through invalid local choice, valid recovery, identity preservation, and one-time proof consumption. Component tests cover name selection, ordinary signup, conflicts, and unrelated errors without restarting authentication. Full workspace tests/type checks, web build, full Go race tests and vet passed. Independent 6.1-sol and Astra reviews approved the fix.
