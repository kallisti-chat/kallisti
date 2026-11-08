# Plugins: access for staff only

## Summary

Access `staff` (admins and moderators). Roles are known per Mumble session;
an HTTP request has none.

## Requirements

- Needs the built-in server. With Murmur, `staff` is refused at start.
- Decide: look the account up among the online users (`mumbled/members.go`),
  or mint a token over the WebSocket and redeem it over HTTP as
  `stream.Tokens` and `recording` do. Write the choice in the DEVLOG.
- A user who is not connected has no role, so no access.

## Acceptance Criteria

- Go tests: admin, moderator, ordinary user, offline user, Murmur mode.
