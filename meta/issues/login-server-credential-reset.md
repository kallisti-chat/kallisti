# Desktop login: clear secrets when the server changes

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

The desktop login component can survive a server change with the previous password and server-password fields intact. Submitting on a new server can send the previous server's secrets there.

Priority: High.

Evidence: Source finding; native end-to-end behavior has not been tested.

## Requirements

- Reset credential and server-specific form state at a server boundary.
- Keep the intended remembered username behavior. Do not send old credentials to the newly selected endpoint.

## Acceptance Criteria

- A component regression enters both secret fields for server A, changes to B without a saved login, and proves both fields are empty.
- Cover Kallisti-to-Kallisti and plain-Mumble transitions, including the same address selected again if needed by the chosen boundary.
- Relevant web tests and type checks pass.
- A delayed signup challenge on A cannot read or send an invitation entered on B. Capture secrets before the wait and recheck the server before sending.
- Challenge progress from an old server cannot change the new server's checking state.

## Notes

- apps/web/src/components/Login.svelte:114
- apps/web/src/App.svelte:368

## Result (2026-11-08)

Server selection now resets both password fields and server-local form policy while preserving the username. Signup captures its password and validates the invitation before proof, then rechecks the server before sending. Guest requests and challenge progress also reject stale continuations.

Verification: component regressions first reproduced retained passwords, stale errors, and a delayed A signup sending B's invitation. These now pass, including API-only replacement, same-API Mumble transitions, same-address reselection, hidden secrets, successful login, and challenge progress. Full workspace tests/type checks and the production web build passed. Native GUI behavior was not tested. Independent 6.1-sol and Astra follow-up reviews found no remaining blockers.
