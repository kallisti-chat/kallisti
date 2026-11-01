# Long names break the chat on phones

## Summary

A name like minazukimidnight@diclonius.princess.wedding made the
message header wider than a phone's screen (a flex row without wrapping
and a name without break points): the time went off screen and the
whole list could pan sideways. Replies to such names did the same.

## Requirements

- The header wraps (the time to the next line, the name itself if it is
  wider than the line); the time stays on one line.
- A reply's name is cut with an ellipsis.
- The message list never scrolls sideways.

## Acceptance Criteria

- Checked in the iOS 27 simulator with long names and a reply.
