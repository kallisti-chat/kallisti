# Messages: resolve nonce retries before new-post cooldowns

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

New-account message rules run before nonce lookup. An immediate retry of an already-stored message is refused by the five-second cooldown instead of confirming the original post.

Priority: Medium.

Evidence: Reproduced with stateful rate accounting matching production.

## Requirements

- Authorize the request before exposing a stored result, then resolve an existing nonce before new-content checks and post accounting.
- Do not consume a cooldown for invalid new content or weaken channel-access checks.

## Acceptance Criteria

- A new account can retry the same stored nonce immediately and gets one stored message and a confirmation.
- A distinct nonce still obeys the cooldown; revoked channel access cannot retrieve a private result.

## Notes

- server/internal/sidecar/hub.go:740,1087
- server/internal/mumbled/messagerules.go:66
- Related: send-after-reconnect.md
