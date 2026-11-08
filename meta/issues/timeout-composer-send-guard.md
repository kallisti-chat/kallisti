# Chat time-outs: disable all write paths and retain drafts

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

A time-out disables the textarea, but an existing draft still has an active send button. Submit clears the draft and reply; the generic time-out error does not return the text needed for restoration.

Priority: Medium.

Evidence: Source finding; textarea and submit guards disagree.

## Requirements

- Use the same cannotWrite policy in submit, attachments, stickers, and other composer write controls.
- Keep unsent draft and reply state when a send is disallowed; keep server enforcement.

## Acceptance Criteria

- Applying a time-out to a composer with a draft prevents sending and retains the text and reply.
- Upload, sticker, poll, and keyboard/button entry paths obey the same policy; writes work again after expiry.

## Notes

- apps/web/src/components/ChatPane.svelte:587,1515,1623
- server/internal/sidecar/moderation.go:74
