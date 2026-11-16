# Agent API: read, post and reply with an access token

## Summary

Programs (agents, scripts, bots) can only read and write chat through the
bridge, which needs a full Mumble handshake; webhooks only post, without an
account, and cannot read or reply. A small HTTP API with personal access
tokens lets them read channels and post or reply as an account.

## Requirements

- Personal access tokens: made in the account settings with a name and a
  scope (read, or read + write), listed, revocable; stored hashed; shown
  once. Not for guest accounts.
- `Authorization: Bearer <token>`; a token acts as its account, with the
  same permissions (channel ACLs, time-outs, guests read-only, message
  rules, slow mode) and rate limits as the account in the client. No Mumble
  connection needed.
- Endpoints: `GET /api/agent/channels` (text channels the account sees),
  `GET /api/agent/channels/{id}/messages?before=&limit=` (history, as the
  client gets it), `POST /api/agent/channels/{id}/messages` `{text, reply}`.
- Messages posted with a token carry a bot marker; the client shows a
  "bot" badge.
- Docs: docs/agent-api.md.

## Acceptance Criteria

- A token reads a channel's history and posts a reply that others see live,
  with the bot badge; a read token cannot post; a revoked token stops at
  once; a channel the account cannot see gives 404.
- Tests for tokens (store, auth), permissions without a session, the
  endpoints; checked against a local server with curl and in the client.

## Notes

- Decided 2026-11-16 with the user. Later, not in this issue: direct
  messages, reactions, an event stream (SSE), an MCP layer.
