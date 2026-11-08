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
