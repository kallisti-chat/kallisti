# Turn off new accounts

## Summary

`REGISTRATION=0` closes the server to new accounts, for a server among
friends that already has its people. The web app has no "Create account"
tab, and the server refuses new accounts by every way: the form, a
fediverse login without an account here, and a guest account kept with a
password or a fediverse link. Logins, guests and Mumble clients are
unchanged. `/api/server` says whether new accounts are taken; an older
server says nothing, and the web app takes that as open.

## Acceptance Criteria

- Server tests: create refused, login still works, a guest joins but
  cannot keep the account, `/api/server` says `registration: false`.
- Web tests: the client reads the switch (missing: open); the `closed`
  reason of a fediverse login has its text.
