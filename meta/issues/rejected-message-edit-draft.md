# Messages: retain edit drafts until the server accepts them

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

saveEdit clears the editor before invoking onEdit. A server refusal loses the proposed text and attachment-removal choices.

Priority: Medium.

Evidence: Source finding; edit state is cleared before the server responds.

## Requirements

- Keep a pending edit snapshot until explicit success or failure, and restore it on rejection.
- Use reliable correlation; coordinate with request-ids.md instead of hiding missing correlation behind a generic send abstraction.

## Acceptance Criteria

- A rejected edit restores text and attachment choices to the correct message.
- An accepted edit closes once; late replies cannot overwrite a different active edit.

## Notes

- apps/web/src/components/ChatPane.svelte:716-733
- server/internal/sidecar/edit.go:69
- Related: request-ids.md

## Result (2026-11-08)

Edits keep text and attachment choices until an explicit correlated result. Requester-only edit results and errors carry request and message IDs. A rejected or pending edit stays with its message; cancellation clears the cached draft, and late replies cannot close another editor. The broader manage-request correlation issue remains open.

Verification: Component and protocol regressions cover rejection, success, cancellation, inactive drafts, attachment choices, and late replies. Final workspace tests, typechecks, web build, full Go race tests, and go vet passed. Independent 6.1-sol and Astra reviews approved the change.
