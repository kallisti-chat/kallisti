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

## Result (2026-11-08)

The composer now uses one writeBlocked policy for send, keyboard, file, sticker, poll, and edit paths. A blocked action keeps the draft, reply, queued files, and open poll form; writes resume after the time-out ends.

Verification: Component regressions cover the entry paths and retained state. The timeout-only snapshot passed 41 tests and typecheck; final workspace tests, typechecks, and web build passed. Independent 6.1-sol and Astra reviews approved the change.
