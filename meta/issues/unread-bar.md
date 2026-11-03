# Unread line, jump to unread, mark unread

## Summary

A user's request (Discord's three features): a "NEW" line above the first
unread message, a bar with "Jump" back to it, and "Mark unread" on a
message to set the line (and the channel's unread state) there.

## Requirements

- The line: when a chat comes into view (opened, or the tab visible
  again) with unread messages from others, a "NEW" line above the first
  one. It stays while the chat is open; messages that come in while it is
  watched do not move it. Gone when the chat closes, after "Mark as read",
  or when we send a message. None without an earlier read position (a
  first visit, guests' first look).
- The bar: while the first unread message is above the view, "N new
  messages since <time>" with "Jump" (loads older pages, at most as other
  jumps) and "Mark as read".
- Mark unread (message menu, phones' sheet too): read up to the message
  before; the channel is unread again and the line moves there. The open
  chat does not mark it read again until it is left (or "Mark as read").
  Stored on the server for accounts (the mark may go back: a new
  request), so the account's other sessions follow; guests: this session.

## Acceptance Criteria

- Tests: the store/sidecar request (back, others' sessions told, access
  checked, guests nothing), the client's reducer, the line's position
  (own messages skipped, none at 0), the bar's text; checked headless.
