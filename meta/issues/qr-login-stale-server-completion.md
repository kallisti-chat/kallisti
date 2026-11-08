# QR login: ignore completion after a server change

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

The QR completion callback captures the API before awaiting `me()`, but checks
only API identity afterward. A desktop switch from Kallisti A to plain Mumble B
can retain A's API object. The delayed callback can then apply A's account and
call `connectAs` using B's current desktop server selection.

Evidence: source finding from the credential-reset review; not reproduced in a
component or native runtime test. The password/invitation isolation fix did not
change QR callbacks.

## Requirements

- Bind QR completion to the server selection that started it, not only the API.
- Ignore a stale account lookup before applying account state or connecting.
- Preserve normal QR completion on the current server. Keep this separate from
  QR grant revocation and the native scanner work.

## Acceptance Criteria

- A component test delays A's `me()` response, switches to plain Mumble B while
  retaining the API object, then resolves A's response. No old account is applied
  and no connection to B is started by that completion.
- A changed-API selection also ignores the stale completion.
- A current-server QR completion still applies the account and connects once.
- Relevant login/QR tests and web type checks pass with fake APIs, without GUI
  apps or a real camera.

## Notes

- `apps/web/src/components/Login.svelte`: `qrDone`, `shown`, `current`, and
  `connectAs`.
- Related completed fix: [Server credential reset](login-server-credential-reset.md).

## Result (2026-11-08)

QR completion captures the API and server selection before the account lookup. A stale lookup cannot apply account state, save a username, or start a connection.

Verification: Component regressions failed before the fix and now pass for same-API address/kind changes, API replacement, and current-server success exactly once. Tests use fake APIs and no camera. Full workspace tests/type checks and the web build passed. Independent reviews: 6.1-sol and Astra; no blockers.
