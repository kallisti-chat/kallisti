# Delete users: also their web account

## Summary

Server settings → Users → Unregister removed only the Mumble registration.
The web account stayed, so the user logged in again with the same name and
password and got a new registration (a user's report). The list has one
action now, "Delete user…": a web account goes with its registration and
sessions (open tabs log out); a Mumble-only user loses the registration, as
before. Messages stay under the old name. Rename is gone from the list.

Only admins (Write in root) delete users and see the list. Built-in server
only (Murmur has no accounts of ours).

## Acceptance Criteria

- Deleting an account's registration deletes the account and ends its
  logins; the user cannot log in again.
- Moderators cannot delete users.
- The list shows "Delete user…" with a clear confirmation, and no Rename.
