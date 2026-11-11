# Flaky Go tests: TestTimeOutsEnd and TestCloseEndsAllConnections

## Summary

Two Go tests fail now and then in CI and turn unrelated pull requests red.

- `mumbled` `TestTimeOutsEnd`: an expired time-out unmutes the user first, then saves it and calls `OnTimeoutsChanged`. The test checks the hook count as soon as the client sees the unmute ("timeouts [], changed 1").
- `bridge` `TestCloseEndsAllConnections`: `Close` can cancel the bridge while its upstream `Dial` is still running. The dial fails with "operation was canceled" and the client gets 1011 "upstream unavailable" instead of 1001 "server shutting down".

## Requirements

- `TestTimeOutsEnd` waits for the hook instead of assuming its order.
- A bridge shutdown during the upstream dial closes the client with 1001.

## Acceptance Criteria

- A bridge test with a dial that blocks until shutdown gets 1001 (fails before the fix).
- Both packages pass repeated runs (`-count` with `-race`).

## Result (2026-11-11)

TimeOutsEnd waits for the hook; the bridge closes with 1001 when a shutdown cancels the dial (new test, red before). 30 runs with -race for both.
