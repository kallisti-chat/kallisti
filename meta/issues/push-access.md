# Push notifications: the right account, and access at send time

## Requirements

- A push goes to the web account whose certificate is the pinged registered user's, not to any account with the user's name (a Mumble registration and a web account may share a name).
- A queued push checks again before it is sent: no push when the user may not read the chat any more or is banned.

## Acceptance Criteria

- Tests: a web account with a pinged user's name but another certificate gets no push.
- Tests: a push queued before the user lost access or was banned is not sent.
