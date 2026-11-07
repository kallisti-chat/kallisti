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

The standalone bot is in `../kallisti-irc`, based on the TV bot's native Mumble approach. No Kallisti server code changed.

## Verification

- `go test -race -count=2 ./...` and `go vet ./...` passed in the bot repository.
- Independent source review approved the fixes; a separate Astra security review found no security blockers.
- Both mappings passed a marker-only end-to-end check in both directions, including after a service restart. Tests did not inspect other users' chat.
- Deployment details remain in the untracked `ops/irc-bridge/` directory.
