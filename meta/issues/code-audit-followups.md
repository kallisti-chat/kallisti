# Code audit follow-ups

## Summary

Track the 23 findings reported in the read-only audit of `53536341`, plus two
related findings from the fix reviews.
Evidence is recorded per issue; source findings still need regression tests.
The three proposed refactors belong to the channel-move, notification-ingress,
and server-scoped preference issues below, not a broad rewrite.

## Requirements

- Start with the channel-deletion access leak, desktop credential reset, and
  federation handoff revocation. Use separate, small fixes.
- Add a failing regression before each correction, then run relevant checks.
- Get an independent review before code commits and the additional security
  review for security or multi-server changes.
- Do not use live services, the real microphone or camera, or visible GUI apps
  for automated tests.

## Acceptance Criteria

- Each reported finding has a linked issue with testable completion criteria.
- Archive each child only after its criteria are verified.
- Close this parent when all child issues are complete.

## Findings

- [x] [Channel deletion: reconcile moved users and audio listeners](channel-deletion-access-cleanup.md)
- [x] [Desktop login: clear secrets when the server changes](login-server-credential-reset.md)
- [x] [Federation login: revoke pending handoffs with authentication](fedi-handoff-revocation.md)
- [x] [Message HTML: prevent styles from covering the application](message-html-layout-styles.md)
- [x] [Audio: isolate ownership of the shared DeepFilterNet node](deepfilternet-capture-ownership.md)
- [x] [Voice: refresh routing after implicit self-undeafen](self-unmute-deaf-routing.md)
- [x] [Streams: preserve both decoded audio channels](stream-stereo-playback.md)
- [x] [Sidecar: recover from outbox overflow without another event](sidecar-outbox-resync.md)
- [x] [Messages: resolve nonce retries before new-post cooldowns](message-nonce-cooldown-retry.md)
- [x] [Uploads: reject interrupted request bodies](upload-truncated-body.md)
- [x] [Threads: delete large histories without exceeding SQLite limits](thread-delete-sql-limit.md)
- [x] [Messages: preserve literal emoji references when editing](personal-emoji-edit-roundtrip.md)
- [x] [Search: distinguish DM and channel targets with the same name](search-target-kind.md)
- [x] [Build: run emoji-data generation on Windows and spaced paths](emoji-data-cli-path.md)
- [x] [Several servers: scope blocks and user audio preferences](server-scoped-user-preferences.md)
- [x] [Notifications: keep clicked links until the target client is ready](notification-click-ingress.md)
- [x] [Connections: bound the initial Mumble handshake](initial-connection-deadline.md)
- [x] [Messages: retain edit drafts until the server accepts them](rejected-message-edit-draft.md)
- [x] [Chat time-outs: disable all write paths and retain drafts](timeout-composer-send-guard.md)
- [ ] [Desktop macOS: declare camera privacy permissions](macos-camera-permissions.md)
- [x] [Desktop voice: keep TCP fallback after a UDP receive error](native-udp-error-fallback.md)
- [x] [Desktop bridge: cancel blocked upstream writes when the client leaves](native-upstream-write-cancellation.md)
- [x] [Federation signup: offer a name when the remote handle is unusable locally](fedi-signup-invalid-local-name.md)
- [x] [QR login: ignore completion after a server change](qr-login-stale-server-completion.md)
- [x] [Channels: remove descendants before their parents](channel-subtree-removal-order.md)

## Notes

- Baseline web tests/type checks and Go race tests/vet passed during the audit.
- Existing issues for message forms, request IDs, and passwordless added-account
  settings are separate; this list does not claim those are new findings.
