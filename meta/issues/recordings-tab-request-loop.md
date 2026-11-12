# Recordings tab asks for the list again and again

## Summary

GitHub #19: with Server settings → Recordings open, the client sends `recordings` requests in a loop; the rate limit then refuses `recording-token`, and Download does nothing. The effect read the prop `recording`, passed as `text.recording`: Svelte tracks the parent's whole text state there, which every answer (and every other text event) replaces.

## Requirements

- Ask when the tab opens, and again only when the recorded channels change.

## Acceptance Criteria

- A component test with a parent that replaces its text state like Client: one request on opening, one more when a recording starts (red before the fix).

## Result (2026-11-12)

The effect depends on the recorded channels by value (a derived string). Test red first (22 requests), green after. Not done: showing the rate-limit error in the panel (suggested in the report).
