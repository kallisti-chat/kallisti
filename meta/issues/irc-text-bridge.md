# IRC text bridge

## Summary

Build a standalone bot that relays text between mapped IRC and Kallisti channels.

## Requirements

- Relay messages in both directions with the original sender's name.
- Support more than one channel mapping, TLS, and reconnects with backoff.
- Prevent relay loops and never forward channel history on reconnect.
- Respect IRC line lengths and flood limits; do not queue old messages across disconnects.
- Test against local protocol fixtures before enabling real channel mappings.

## Acceptance criteria

- Automated tests cover routing, formatting, loop prevention, and reconnect behavior.
- Two mapped channels exchange messages without cross-channel delivery.
- The bot can run separately from the Kallisti server without server code changes.

## Notes

The existing TV and jukebox bots provide examples of native Mumble clients. Work belongs in a sibling bot repository, not the Kallisti server.
