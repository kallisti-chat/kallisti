# Names with one character

## Summary

Account names need 2 to 32 characters (names.Check). The user wants 1 to
32. Names from other servers already allow one character before the "@".

## Requirements

- names.Check takes 1 to 32 characters; everything else stays.

## Acceptance Criteria

- Tests: a one-character name is valid, an empty one and 33 characters
  are not.
