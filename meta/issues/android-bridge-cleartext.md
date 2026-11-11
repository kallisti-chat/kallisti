# Android release app cannot connect through the local bridge

## Summary

After login, the Android app connects to a Kallisti server with a Mumble UDP port through its local bridge at `ws://127.0.0.1:<port>`. Release builds set `usesCleartextTraffic="false"`, so the WebView refuses that socket (`net::ERR_CLEARTEXT_NOT_PERMITTED`). The close has no reason, so the client does not fall back to a direct connection and shows "Lost the connection to the server." Debug builds allow cleartext, so they work. Reported by flghl on chat.kallisti.chat after QR login (v0.13.1); reproduced in the emulator with the v0.13.1 release APK.

## Requirements

- Allow cleartext only to the local bridge (127.0.0.1) in release builds.
- Keep cleartext for the dev server in debug builds.

## Acceptance Criteria

- A release APK logs in to a Kallisti server with a UDP port and connects through the bridge in the emulator.
- Other cleartext hosts stay blocked in release builds.

## Notes

- Separate: the client could fall back to a direct connection when the bridge socket fails before it opens.

## Result (2026-11-10)

Network security config: cleartext only to 127.0.0.1 in release, everywhere in debug. Verified in the emulator with a debug-signed release APK (QR login, connected; other cleartext hosts blocked).
