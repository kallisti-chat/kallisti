# Unregister deletes the web account

## Summary

Server settings → Users → Unregister removed only the Mumble registration.
The web account stayed, so the user logged in again with the same name and
password and got a new registration (a user's report). For a registration
of a web account the button is "Delete account…": the account goes with
its sessions (open tabs log out). Messages stay under the old name.

Only admins (Write in root) delete accounts; moderators keep Unregister for
Mumble-only registrations. Built-in server only (Murmur has no accounts of
ours).

## Acceptance Criteria

- Unregistering an account's registration deletes the account and ends
  its logins; the user cannot log in again.
- A moderator cannot delete an account.
- The Users list shows "Delete account…" for accounts, with a clear
  confirmation.
