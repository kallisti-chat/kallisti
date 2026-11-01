# Older messages load while scrolling up

## Summary

Text channels show "Load older messages" at the top. The user wants
infinite scroll with a sensible load window.

## Requirements

- Older messages load by themselves when the reader comes within 1.5
  screen heights of the top (the next page is there before the reader);
  again after a page while still that close (tall screens, short pages).
- Not while a page is loading, while a jump to a message pages on its
  own, or when there is nothing older.
- The reading position stays when a page comes in (as with the button).
- The top shows "Loading older messages…" while a page loads; the
  channel's start as before when there is nothing older.
- A page has 50 messages (was 30).

## Acceptance Criteria

- Unit test of the window rule; checked headless: scrolling up loads
  pages, the position stays, the start shows at the end.
