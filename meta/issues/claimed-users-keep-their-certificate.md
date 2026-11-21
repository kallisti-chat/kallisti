# Claimed users keep their certificate

## Summary

After a claim link (see recovery-link-for-imported-users), the registration
has the account's certificate, and the user's own Mumble certificate stops
working: the Mumble app needs the password. The user's certificate should
keep working by itself too.

## Requirements

- A registration can have further certificates (mumble_user_certs); a
  client with one logs in as that registration, as with its own.
- A claim keeps the old certificate as such a further one.
- A password change, logging out everywhere and a recovery end them (and
  their clients); deleting the user deletes them.

## Acceptance Criteria

- Tests: login with the further certificate after a claim; gone after
  ending logins and after deleting the user.
- Tried on test.kallisti.chat: the old certificate logs in after a claim
  without a password.
- docs/migrating-from-murmur.md updated.
