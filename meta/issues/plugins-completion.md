# Plugins: `!` completion in the composer

## Summary

The manifest's command list feeds completion in the composer, so people who
stay in chat do not have to learn the command names either.

## Requirements

- `lib/suggest.ts`: a trigger for the plugin's prefix at the start of a
  message, offering names, aliases and the help line; arguments with
  choices offer the choices.
- Only for plugins the user may use, and only in the channels the plugin
  is set for. Admin-only commands only for staff.
- No change to what is sent: it is still the text the user types.

## Acceptance Criteria

- Vitest: the trigger, filtering, choices, and no suggestion for a plugin
  the user may not use.
