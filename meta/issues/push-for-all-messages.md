# Push for channels set to "All messages"

## Summary

A channel's notification level "All messages" notifies only while the app
is open. The server sends a push only for pings (mention, reply, @everyone,
direct message).

## Requirements

- A browser tells the server, with its push subscription, which chats it
  has set to "All messages" (not the muted ones).
- A message without a ping in such a chat goes to those browsers, when the
  user is not connected. A thread takes its channel's setting unless it has
  one of its own.
- The same limits as for pings: may read the chat, not banned, not in do
  not disturb, not the author, not a blocked author, one push per chat per
  minute.
- A browser that has the chat on "Mentions only" gets no push for such a
  message, also when another browser of the same account does.

## Acceptance Criteria

- Tests on store, push and sidecar for the rules above.
- On the test instance: a closed tab with "All messages" on a channel gets
  a push for a plain message.

## Notes

- The text: "Message in #general from alice" (never the message).
