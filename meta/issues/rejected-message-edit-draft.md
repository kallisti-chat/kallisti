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
