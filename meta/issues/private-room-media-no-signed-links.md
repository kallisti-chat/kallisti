# Private-room media: a mode without signed links

## Requirements

- `PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS=1` (with `EXPERIMENTAL_PRIVATE_ROOM_MEDIA=1`): the server signs no file links; every file request goes through the session check (slower, nothing works without a session).
- Accepted (user, 2026-11-06): private files are broken in the apps and other servers' tabs in this mode; the docs say so.
- Helm: a value for it.

## Acceptance Criteria

- Config: refused without the experiment and with Murmur.
- With it: messages carry plain links; signed queries are not accepted.
