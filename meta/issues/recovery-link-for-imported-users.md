# Recovery link for imported Mumble users

## Summary

Users imported from Murmur (and users who registered in a Mumble app) have a
Mumble registration with their client's certificate, but no Kallisti account,
so they cannot log in on the web. feld asked for a way to convert his old
account (#kallisti-dev 12835).

## Requirements

- An admin's "Recovery link" also works for such a registration.
- Opening the link sets a password: it makes a Kallisti account with the
  registration's name, and the registration moves to the account's
  certificate. The registration keeps its id: groups, channel rights, avatar,
  comment and messages stay.
- Clients with the old certificate end; Mumble apps log in with name and the
  new password afterwards.
- Same admin rules as the recovery link (admins; SuperUser for admins); links
  end when roles change.
- No account is made before the link is used.

## Acceptance Criteria

- Tests: link for an imported user, use it, the account logs in on the web
  and the Mumble user id is the same; a used or expired link fails; a name
  that has an account already is refused.
- Tried on test.kallisti.chat with an imported (certificate) user.
- docs/migrating-from-murmur.md says how.
