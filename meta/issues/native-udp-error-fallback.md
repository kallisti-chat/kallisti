# Desktop voice: keep TCP fallback after a UDP receive error

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

An unhandled UDP receive error parks the entire UDP loop forever. That also stops pings and fallback checks, so a receive-only user may never switch the server back to TCP voice.

Priority: Medium.

Evidence: Source finding; native network-error behavior was not exercised.

## Requirements

- Preserve fallback checks after abandoning the receive socket, or force tunneled fallback immediately.
- Keep the TCP session and existing expected ICMP handling.

## Acceptance Criteria

- An injected UDP receive error after UDP becomes active causes bounded TCP fallback without outgoing speech.
- UDP success, expiry, and expected ICMP error tests still pass.

## Notes

- apps/desktop/src-tauri/src/bridge.rs:450-469
- apps/desktop/src-tauri/src/udp.rs:106
