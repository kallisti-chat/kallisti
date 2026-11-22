# Agent API

Programs (agents, scripts, bots) can read text channels, follow them live,
post, reply and react as a Kallisti account over plain HTTP, with a personal access token. They
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
(401: no valid token, 403: a read token posts, or the account is banned, 404: unknown channel or one
the account cannot see, 400: the post was refused, with the reason, 429:
too many requests or slow mode: wait and try again).

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

Needs a token that may post. Body: `{"text": "…", "reply": 12017, "nonce": "…"}`
(`reply` is optional: the message to answer, in the same channel; `nonce`
is optional: send the same one on a retry and the post is stored once). The
answer is `201` with the stored message: `{"message": {…}}`. Others see it
at once, as any message.

### `POST /api/agent/channels/{id}/messages/{message}/reactions`

Needs a token that may post. Body: `{"emoji": "👍", "on": true}`; `on` is
optional, `false` removes the reaction. The emoji is one the app offers:
a Unicode emoji, or a custom one of the server as `:name:`. The answer is
`204`.

### `GET /api/agent/events`

A stream of [server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html)
for the chats the account may read. The first event is `ready`. Then,
each with the JSON the app gets:

| Event | Data |
|---|---|
| `message` | `{"message": {…}}`: a new message |
| `message-edited` | `{"message": {…}}`: its new state |
| `message-deleted` | `{"channel": 3, "id": 12017}` |
| `messages-deleted` | `{"channel": 3, "ids": […]}`: several at once |
| `reactions` | `{"channel": 3, "message": 12017, "reactions": […]}` |

Lines that start with `:` are keep-alives. An account has at most 4
streams. The server ends a stream when the token ends or the account is
banned (within about half a minute), and when the program reads too slowly. Events are not kept: after a break, connect
again and read what you missed with the messages endpoint.

## Example

```sh
TOKEN=kallisti_pat_…
curl -s -H "Authorization: Bearer $TOKEN" https://chat.example.org/api/agent/channels
curl -s -H "Authorization: Bearer $TOKEN" "https://chat.example.org/api/agent/channels/3/messages?limit=20"
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"text": "Hello from a script", "reply": 12017}' \
  https://chat.example.org/api/agent/channels/3/messages
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"emoji": "👀"}' \
  https://chat.example.org/api/agent/channels/3/messages/12017/reactions
curl -sN -H "Authorization: Bearer $TOKEN" https://chat.example.org/api/agent/events
```

Not yet: files, and reading or sending direct messages (the event stream
already has the account's own).
