# Bridge: a slow browser loses its connection before the ping grace

## Summary

The bridge pings the browser every KeepAlive with a timeout of one KeepAlive. coder/websocket closes the whole connection when a write's context ends while the frame is being written. With short ticks (tests) a browser that reads slowly loses its connection after one KeepAlive instead of the PingGrace; in production the library's own 5 s limit on control frame writes applies first, so there the change only lets the pong wait the full grace. CI showed it as a flaky `TestSuspendedBrowsersSurviveAFullSocket` ("EOF" after 2.6 MB).

## Requirements

- A ping may wait as long as the grace still allows.
- The Mumble pings upstream keep their pace while a browser ping waits.

## Acceptance Criteria

- The test with a reader slower than the grace fails before the fix and passes after it, also with `-race` and repeated runs.

## Result (2026-11-12)

One ping at a time in a goroutine, waiting up to the rest of the grace; Mumble pings go on every tick. The slow-reader test failed every run before and passes with -race after; independent review found nothing significant. Left: a ping frame stuck mid-write for over 5 s (the library's limit) still closes the connection.
