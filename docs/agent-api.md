# Agent API

Programs (agents, scripts, bots) can read text channels and post or reply
as a Kallisti account over plain HTTP, with a personal access token. They
need no Mumble connection. This needs the built-in Mumble server.

## Tokens

Make a token in **Settings → Account → Access tokens**: give it a name and
choose whether it may post ("May post"); without that it only reads. The
token shows once: copy it then. Guest accounts have no tokens; an account
has at most 20.

A token acts as its account, with the same rights and limits as the account
in the app: channel permissions, time-outs, slow mode, message rules, and
the rate limit (5 requests a second, bursts of 10). The account must have
joined the server once (with the app or a Mumble client), so that its
registration exists.

Posts made with a token carry a **Bot** badge in the app, so people see
that a program wrote them.

All of an account's tokens end when its password changes, when it logs out
everywhere, when an admin's recovery link is used, and when the account is
deleted. Remove a single token in the same settings section.

Send the token in the `Authorization` header:

```text
Authorization: Bearer kallisti_pat_…
```

## Endpoints

All answers are JSON; errors are `{"error": "…"}` with an HTTP status
(401: no valid token, 403: a read token posts, 404: unknown channel or one
the account cannot see, 400: the post was refused, with the reason, 429:
too many requests).

### `GET /api/agent/channels`

The text channels the account sees:

```json
{"channels": [{"id": 3, "name": "kallisti-dev", "topic": "…"}]}
```

### `GET /api/agent/channels/{id}/messages?before=&limit=`

The newest messages, oldest first; `limit` up to 100 (default 50).
`before` (a message id) pages back; `more` tells whether older ones exist.

```json
{"messages": [{"id": 12017, "channel": 3, "author": "flghl", "verified": true,
  "text": "…", "at": 1794771000000, "reply": {"id": 12001, "author": "kaia", "text": "…"}}],
 "more": true}
```

`at` and `edited` are Unix milliseconds; `bot` marks posts made with a
token; files are in `uploads` (URLs relative to the server).

### `POST /api/agent/channels/{id}/messages`

Needs a token that may post. Body: `{"text": "…", "reply": 12017}`
(`reply` is optional: the message to answer, in the same channel). The
answer is `201` with the stored message: `{"message": {…}}`. Others see it
at once, as any message.

## Example

```sh
TOKEN=kallisti_pat_…
curl -s -H "Authorization: Bearer $TOKEN" https://chat.example.org/api/agent/channels
curl -s -H "Authorization: Bearer $TOKEN" "https://chat.example.org/api/agent/channels/3/messages?limit=20"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"text": "Hello from a script", "reply": 12017}' \
  https://chat.example.org/api/agent/channels/3/messages
```

Not yet: direct messages, reactions, files, and a stream of new messages.
