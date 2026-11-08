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

## Notes

- apps/web/src/components/Login.svelte:114
- apps/web/src/App.svelte:368
