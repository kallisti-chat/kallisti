# Federation login: revoke pending handoffs with authentication

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

A pending federation handoff keeps an account snapshot. Unlinking the identity, changing the password, or logging out all sessions does not prevent that handoff from creating a new 30-day session during its two-minute lifetime (five minutes with consent).

Priority: High.

Evidence: Reproduced for unlink, password change, and logout-all.

## Requirements

- Bind handoff redemption to current identity and authentication validity, and invalidate it on the relevant revocation paths.
- Preserve one-use, expiry, origin, consent, and blocked-home checks.
- Do not introduce a new token type or change unrelated provider authority.

## Acceptance Criteria

- Regression tests reject preexisting handoffs after identity unlink, password change, and logout-all.
- Normal handoff redemption, consent, expiry, and one-use behavior still pass.
- Run auth tests with the race detector and obtain the additional security review.

## Notes

- server/internal/auth/fedi.go:144,706,763,803
- server/internal/auth/auth.go:427,734,797
