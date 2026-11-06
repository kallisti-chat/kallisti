# Private-room media (PR #18) without broken apps or slow reads

## Requirements

PR #18 (cortexobscura) gates `/files/` with `EXPERIMENTAL_PRIVATE_ROOM_MEDIA=1`. Review findings to fix on top of it:

1. A certificate-hash ACL deny does not block files (the reader has no hash).
2. The link-image cache keeps serving a copy after the room became private.
3. The gate copies the chat access rules and differs: voice chats (MayRead, not only Enter), text channels without a mirror. Reuse `userMay` / `offlineAccess`.
4. Apps and other-server tabs send no cookie: every private-room image is a 404 there.
5. Each file request costs many queries (full message load, session lookup even for public rooms).

Decisions (user, 2026-11-05):

- Signed links as Discord: messages carry `/files/<key>/<name>?ex=..&sig=..`, valid 24 hours; the server checks only the HMAC. A client asks for fresh links when they expired.
- A link without a signature: the session check (one query, anonymous check first).
- Merge the PR locally, fix on top, merge to main.

## Acceptance Criteria

- Tests for each finding.
- Private-room images load in the apps (bearer session) and in other-server tabs.
- A signed request does no database query besides the upload itself.
- Flag off: unchanged, except two fixes that hold for all servers: link previews take no picture from answers marked private or no-store (a shared cache must not keep them), and offline access (pushes) counts the certificate's own ACL entries.

## Review results (subagent, Astra)

- Fixed: a send retry with an old nonce gave a removed member their private message with fresh links (now matched in the same channel only); a refresh removed link cards; an unanswered refresh was never tried again (now once an hour).
- Left (rare: a chat open for about a day, within one hour): a refresh restarts a playing video or audio file; an open picture viewer keeps the old links; a shown spoiler hides again. Pins and search results get fresh links only when the message is in the open chat.
- Left: a link without a signature costs a few queries more for threads and voice chats (rare now).
