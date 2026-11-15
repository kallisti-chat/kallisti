# OIDC login (PR #28): finish and merge

## Summary

cortexobscura's #28 (GitHub #26) adds one operator-configured OpenID
Connect provider. The review found no blockers; we fix the points
ourselves (no review round), then merge.

## Requirements

- Docs: OIDC-only accounts that lose their provider need the admin
  recovery link (admin-recovery-link.md); behind a TLS proxy
  TRUST_FORWARDED_FOR is needed.
- One validation for issuer and callback (config and auth both do it);
  the one-time proofs and the cookie shared with the Fedi login.
- No Makefile; the gofmt check goes into CI instead. Captcha fix and
  voice diagnostics are on main already.
- "link" with an identity already linked is refused before the provider
  round-trip; unlink without an identity gives a fitting error.
- UnlinkFedi counts an OIDC identity as another login.

## Acceptance Criteria

- Server and web tests pass; the OIDC flow still works against a real
  provider (podman).
- Merged to main, both remotes.
