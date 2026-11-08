# Request ids for manage requests

## Summary

Save feedback (save-feedback.md) matches server answers to waiting
controls by order, since requests carry no id. Edge cases: a refusal of a
request that does not wait can land on a waiting control; another admin's
broadcast can confirm early; a refusal after a dialog closed is lost;
non-manage errors leave the control waiting until its 5 s timeout. Fix:
an optional request id the client sends and the server echoes on the
answer or error; the client matches by id.

## Acceptance Criteria

- Go tests: the id comes back on results and errors (manage and others).
- Web: matching by id; the order-based fallback only for old servers.

## Notes

- The [edit-draft fix](rejected-message-edit-draft.md) adds optional `requestId`
  correlation for edits. Requester-only `edit-result` replies carry the message
  ID; edit errors carry `requestId` and `editMessage`, including early refusals.
  Broadcasts do not confirm edits. This does not complete manage-request
  correlation; its existing order-based path is unchanged.
