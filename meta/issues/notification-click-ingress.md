# Notifications: keep clicked links until the target client is ready

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

The service worker sends one open message to an existing window. The listener lives in Client, which may be absent during login or connection setup, so the click is lost.

Priority: Medium.

Evidence: Source finding; no end-to-end service-worker test was run.

## Requirements

- Move worker-message ingress to App and reuse createServerLinks rather than adding another router.
- Retain owner and server validation; leave message lookup and jumping in Client.

## Acceptance Criteria

- A worker click before Client mounts is consumed once when the target session becomes ready.
- Invalid or unknown targets are rejected, owner changes clear pending links, and listeners are removed on disposal.
- Several mounted server views do not handle the click more than once.

## Notes

- apps/web/src/sw.ts:39-49
- apps/web/src/components/Client.svelte:875
- apps/web/src/lib/serverLinks.svelte.ts:46
