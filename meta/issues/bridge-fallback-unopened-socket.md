# Apps: connect straight when the bridge socket never opens

## Summary

The apps fall back to a direct WebSocket only when the bridge closes with "cannot reach …". When the web view cannot open the socket to the bridge at all (Android release builds refused cleartext to 127.0.0.1), the close has no reason, so the app never connected.

## Requirements

- A bridge socket that closed before it opened, not closed by the client, counts as a bridge failure: the next try goes straight.
- Client closes before open (a newer attempt, wake, leave) do not count.

## Acceptance Criteria

- Unit tests for the connection flag, bridgeFailed and the session's straight retry.
- An Android release APK without the cleartext exception connects straight.

## Result (2026-11-10)

`failedToOpen` on connections; bridgeFailed uses it. Tests added; checked in the emulator with a release APK built without the network security config: QR login, then connected straight.
