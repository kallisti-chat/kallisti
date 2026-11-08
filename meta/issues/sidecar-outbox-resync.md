# Sidecar: recover from outbox overflow without another event

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Outbox overflow marks the connection lagged, but a later send is needed to enqueue resync. A quiet connection can remain out of date after its queue drains.

Priority: Medium.

Evidence: Reproduced by overflowing and draining the outbox with no subsequent event.

## Requirements

- Trigger resync or close the connection without relying on a future application event.
- Keep recovery bounded and preserve backpressure.

## Acceptance Criteria

- Overflow, drain, and send no additional event: the client receives recovery or disconnects.
- Existing sidecar and bridge tests pass.

## Notes

- server/internal/sidecar/hub.go:1379
- server/internal/bridge/bridge.go:468
- Earlier completed work: text-channels-robustness.md
