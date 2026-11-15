# Admin recovery link

## Summary

An account without a usable login is lost: an OIDC-only account whose
provider was removed or changed its issuer (#28 asks a fresh provider
login before a first password), or anyone who forgot their password.
Kallisti has no admin way to help. With Fedi login off, a first password
needs no proof at all instead (anyone with an open session can set one).

## Requirements

- An admin makes a one-time recovery link for a registered account (in
  Registered users). Valid 24 h, single use; a new link replaces the old
  one. Opening it sets a new password and ends the account's other
  sessions.
- Admins only; not for another admin's account. Logged on the server.
- Fedi login off: a first password needs a proof like with OIDC (no proof,
  no password); the recovery link is the way out.

## Acceptance Criteria

- Admin makes a link; the user sets a password with it and logs in; the
  link does not work twice or after 24 h; other sessions ended.
- Non-admins and links for admin accounts are refused.
- With Fedi off, a Fedi-only account cannot set a first password without
  a proof.

## Notes

- Decided 2026-11-15 with the user, while reviewing #28 (OIDC).
