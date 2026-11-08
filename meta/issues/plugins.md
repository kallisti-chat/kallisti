# Plugins: a small UI for bots and tools

## Summary

A bot such as gotamusique is controlled by typing `!commands`. Plugins give
it a small panel in the web client: buttons, a volume slider, the queue,
live state. A plugin describes its panel in a manifest; Kallisti draws it
and calls the plugin through its own server. gotamusique is the first plugin.

Design and reasons: [plugin-system](../research/plugin-system.md); the
manifest: [plugin-manifest](../research/plugin-manifest.md). Web client
only (the desktop app gets it with the bundled client; Android is out of
scope). Decided in the design: a declarative panel first, the sandboxed
iframe only if a plugin needs it; the plugin's access is set in Kallisti's
configuration, not in the manifest.

The bot's side (an HTTP API with `/state`, `/commands`, `/events`) is
gotamusique's tasks 3-07 and 3-08, in its own repository.

## Requirements

- Sub-issues, in this order: the chat-only prototype, the proxy and
  configuration, the renderer, command completion, role gating, and
  (only if needed) the iframe.
- Open questions to settle on the way (see the research): panel per server
  or per channel, guests, hiding the bot's replies, Jukebox.

## Acceptance Criteria

- [ ] The sub-issues are done; a gotamusique panel works from the manifest
  the bot serves, and a command added to the bot shows up without a
  Kallisti change.
