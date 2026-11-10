# Chat: clear stale errors when sending another message

## Summary

A rejected oversized text message sets the chat error banner. A later accepted message does not clear it; a history response can make it disappear. The error-to-success sequence is reproduced in an isolated reducer test.

## Requirements

- Clear the stale chat error when the user makes a new text-send attempt.
- Preserve server length validation, new rejection messages, and pending attachment/edit error state.
- Do not clear errors because an unrelated incoming message arrives.
- Do not add a new wire-correlation scheme for this UI reset.

## Acceptance Criteria

- A regression first shows that retrying a send retains the old banner, then passes after the fix.
- A valid retry stays clear after acceptance, while a new rejection is displayed.
- Incoming messages alone do not dismiss the error; pending attachment/edit behavior remains unchanged.
- Relevant connection tests, workspace tests/typechecks, and the web build pass.

## Result (2026-11-10)

Plain and nonce-confirmed message sends now clear only the old chat banner when a new send can be attempted. Incoming messages do not dismiss errors, disconnected attempts keep the banner, and a later rejection is still displayed. Server length validation and per-request pending state are unchanged.

Verification: Four retry regressions failed before the fix; all seven send-error tests now pass, together with the three existing edit-correlation tests using the shared socket fixture. Full workspace tests/typechecks and the web build passed. Independent 6.1-sol review approved the fix. Tests use fake server events, not a live service.
