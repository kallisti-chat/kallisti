# Invite-only registration

## Summary

GitHub PR #11 (cortexobscura): an admin switch closes public signup, and
account invitation links admit new accounts. We finish it ourselves.

## Requirements

- Off by default, also after an upgrade. With it on: password and
  fediverse signups need a valid invitation; new guests (web and Mumble)
  are refused. Existing accounts, sessions and registered Mumble users keep
  access. Guests from before the switch may still become permanent (the
  user's decision).
- Invitation use, account creation and the policy check in one
  transaction; a failed creation gives the use back.
- Only admins manage the switch and the links.
- Review fixes:
  - Docs: bots without a registered certificate (Jukebox, Kallisti TV) are
    refused when the switch is on.
  - A revoke never deletes a newer invitation (ids are not used again).
  - With registration open, an invitation is not checked and not used.
  - An invitation carried over a fediverse login does not replace a
    waiting channel invitation, ?join or ?authorize.
  - Cleanups: one admission function for the early check and the
    transaction, one create function each (no ...WithInvite twins), no
    registrationMu, /api/server leaves out inviteOnly when the read fails,
    the invitation field once in Login.svelte.
  - No test-setup files (deploy/compose.invite-test.yml,
    docs/testing-invite-only.md); a short section in docs/configuration.md.

## Acceptance Criteria

- Tests for each review fix; all Go and web tests pass.
- Checked on the test servers by the user before production.
