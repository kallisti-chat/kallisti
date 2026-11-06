# The unread bar goes once seen

## Requirements

- User report (shibao): after scrolling up and back down to the newest messages, the "N new messages since" bar is still there. Discord removes it.
- The bar goes for the rest of the visit once the reader saw the "New" line, or scrolled back down to the newest messages. The "New" line stays until the chat closes (as now, as Discord).

## Acceptance Criteria

- Unit test for the rule; checked in headless Chromium.
