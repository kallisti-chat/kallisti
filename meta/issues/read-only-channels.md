# Read-only text channels

## Summary

kaia (#kallisti-dev 13636): channels where only moderators or admins (or chosen people) post, such as announcements. The server already checks the Mumble ACL "Text message" for every post (MayWrite); the web client has no setting for it.

## Requirements

- A "Read only" setting for a channel, as "Listen only" for Speak: deny Text message for all, allow it for the chosen people; staff can always post.
- Others read, react if allowed, and see why they cannot write (the message box says so).
- Mumble clients get the same rule (it is the ACL).

## Acceptance Criteria

- An admin turns "Read only" on in the channel settings; a normal user cannot post (web, agent API, Mumble client) and sees a note; staff and the chosen people can.
- Turning it off restores the old rights.
- Server and client tests.
