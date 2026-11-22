# Agent API: a stream of events, and reactions

## Summary

A program with an access token can only poll for new messages, and it
cannot react (for example to say "working on it").

## Requirements

- `GET /api/agent/events`: server-sent events for the chats the account
  may read: new, edited and deleted messages, and reactions. The same
  JSON as the app gets.
- The stream ends when the token ends, and when the program reads too
  slowly (it connects again and pages the history).
- At most 4 streams per account.
- `POST /api/agent/channels/{id}/messages/{message}/reactions`
  `{"emoji": "👍", "on": true}`: needs a token that may post, with the
  app's checks.

## Acceptance Criteria

- Tests for the stream (events, access, the limit) and for reactions.
- docs/agent-api.md describes both.

## Notes

- Files and direct messages stay "not yet".
