# Plugins: the chat-only prototype

## Summary

A music panel in the right column that only sends `!commands` into the
channel (no server change, no manifest). It shows the layout and the value
early, and stays as the fallback for bots with no API (`chat` actions).

## Requirements

- A new `Side` value (`lib/mobile.ts`) and a branch in the right column of
  `Client.svelte`; a header button next to pins and threads; works in the
  phone drawer.
- Buttons send through the normal send path (`connection.sendText` /
  `sendToTextChannel`), so time-outs, slow mode and filters apply.
- The bot's user name and command prefix come from a local setting for now.
- Logic (building the command text, escaping) in `lib/`, as pure functions.
- Texts in `i18n/en` (and the other languages as usual).

## Acceptance Criteria

- Vitest: command building and escaping; the side switch; the phone panel.
- Manual check on desktop width and on a phone: the panel opens, the
  buttons make the bot act.
