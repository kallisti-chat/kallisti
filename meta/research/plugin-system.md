# Plugins: a small UI for bots and tools inside the web client (2026-11-08)

Analysis and a proposal. Nothing here is built. Web client only: the desktop
app bundles the web client and gets the result for free later; Android is out
of scope.

## The need

Bots such as gotamusique (a Mumble music bot) are controlled by typing
`!commands` in a channel. People have to learn the commands, and the bot's
state (what plays, the queue, the volume) is only visible as chat text.

Wanted: a small panel in the client with buttons, a volume slider, the queue
and so on, and a way for any bot or tool to describe such a panel without
changing Kallisti each time. gotamusique is the first plugin. Kallisti's own
Jukebox ([kallisti-jukebox](https://github.com/kallisti-chat/kallisti-jukebox))
could adopt the same contract later; that is not part of this.

Rules, as in [discord-comparison.md](discord-comparison.md): it is Mumble, it
is easy to use, and easy to use wins when they conflict. Plus one of ours:
Kallisti's strict page policy stays strict.

## What the code gives us

- **Panel slot.** The right column in `Client.svelte` is an if/else chain on
  `side` (`Side` in `lib/mobile.ts`: members, search, pins, threads). Another
  value is the cheapest slot, and it reuses the phone drawer.
- **Iframes are blocked today.** The page policy (`server/internal/site/site.go`)
  has no `frame-src`, so it falls back to `default-src 'self'`: same-origin
  frames load, foreign ones do not. `frame-ancestors 'none'` stays either way.
- **`/api/v1/` is taken.** `auth.Paths` claims it for the Mastodon-style login
  provider. The plugin proxy needs another prefix: `/api/plugins/<id>/`.
- **Who is calling is easy to know.** With the built-in server the web account
  name is the Mumble user name (`cmd/server/main.go`, `builtin.go`), so the
  server can send the name to the bot. Guests are temporary accounts.
- **Roles are not.** `IsAdmin`, `MayManage` and `ModeratesIn` work on a Mumble
  session, and a plain HTTP request has none. Options: look the account up
  among the online users (`mumbled/members.go`), or mint a token over the
  WebSocket and redeem it over HTTP, as `stream.Tokens` and `recording` do.
  Both need the built-in server (not Murmur mode).
- **No streaming proxy exists.** The server has no `WriteTimeout`, so
  long-lived responses work. A proxy needs flush per chunk, no total client
  timeout, and to stop the upstream when the browser leaves.
- **The SSRF guard does not fit.** `netguard.Public` rejects loopback, and
  loopback is where the bot lives. The upstream is set by the admin, not by a
  user, so a plain client pinned to that host and port is right.
- **Config is environment variables only** (`internal/config`), with no nested
  structure.
- **The client knows no bots, no slash commands, no flags.** Composer
  suggestions are `@`, `#` and `:emoji` (`lib/suggest.ts`). A bot's replies
  are ordinary messages from its user name. Webhooks are the only precedent
  for bot-like posts.

## Options

### A. How a plugin's UI is drawn

| | What | For | Against |
|---|---|---|---|
| A1 | **Declarative**: the plugin sends a description (buttons, slider, list, form); Kallisti draws it with its own components | No foreign code in the page, no policy change. Themes, languages, keyboard use and the phone layout come free | Only what the vocabulary says. Needs a good vocabulary |
| A2 | **Sandboxed iframe**: the plugin serves a page through our proxy; `sandbox` without `allow-same-origin`; a `postMessage` bridge for what it may ask | Any UI | Needs `frame-src 'self'`; theming, size and the bridge are work; a bigger attack surface |
| A3 | Script bundle loaded into the page | Any UI, fast | Foreign code with the user's session. Contradicts `script-src 'self'`. **Rejected** |
| A4 | **Chat only**: buttons send `!commands`, state is read from the bot's replies | No server change, any bot works today | State is guessed from text; no queue or slider that stays true |

**Recommendation: A1 as the main path, A4 as the first prototype and the
fallback for bots without an API, A2 later and only when a plugin needs
something A1 cannot say.** A3 is out.

### B. How the plugin's service is reached

1. **Through the Kallisti server** at `/api/plugins/<id>/*` (recommended). The
   server checks who may use it, adds the secret and the user name, and
   forwards the event stream. The browser only talks to its own origin: no
   CORS, no policy change, and the secret never reaches a browser.
2. Browser straight to the bot. Rejected: the secret would be in the browser,
   CORS, and a loopback-only bot cannot be reached.
3. Through chat only (A4).

### C. Where the description (manifest) comes from

1. **The plugin serves it** at `GET /manifest` (recommended). The bot owns its
   UI and its command list, so a new command appears without a Kallisti
   release. Kallisti caches it and asks again with `ETag`.
2. A file next to Kallisti's configuration. Simple, but drifts from the bot.
3. Both: the plugin's, with an optional local override. Mentioned, not planned.

### D. Configuration

Environment variables only, so: (a) indexed variables, `PLUGINS=gotamusique`
and `PLUGIN_GOTAMUSIQUE_URL`, `_TOKEN` and so on (recommended; same style as
the rest), (b) one JSON value, (c) a `PLUGINS_FILE`.

Checks at start: id characters and uniqueness; URL is http or https, no user
info; the token is not empty and never logged; a plugin that is down does not
stop Kallisti (the panel shows "not reachable").

### E. Who may use a plugin

Per plugin, in the server's configuration (not in the manifest: the plugin
must not be able to widen its own access):

- `members`: any logged-in non-guest account (default).
- `everyone`: also guests.
- `staff`: admins and moderators. Needs the built-in server and a session
  lookup.

Optionally a channel list, so the panel only shows in the bot's channel.
Commands the bot marks admin-only are hidden in the panel, and the bot
enforces them anyway using the forwarded user name. The server is the gate;
the panel hiding things is a courtesy.

### F. Hooking into commands and chat

- Actions in the manifest are of two kinds: `http` (call the plugin through
  the proxy) and `chat` (send a templated message such as `!volume {value}`
  into a channel, for bots with no API).
- **Composer help.** The manifest's command list feeds `!` completion in the
  composer, so people who stay in chat still do not have to learn the names.
  This is the part that answers "we don't want to learn the commands" most
  directly, and it works without opening the panel.
- **Chat noise** (follow-up, not first version): collapse the bot's replies
  while the panel is open, at the hook `chatItems(..., blocked)` in `lib/chat.ts`.

### G. Live state

The open panel subscribes to the plugin's event stream through the proxy and
falls back to asking for the state on a timer if the stream drops. The
manifest binds the UI to paths in the state. A closed panel holds no
connection.

## Security

| Threat | Answer |
|---|---|
| A plugin sends script in a label or an image URL | The renderer only draws text; no HTML from a manifest. Images: only through our own origin or the existing `img-src`, and never from an `http:` URL |
| A plugin's manifest is huge or deeply nested | Size and depth limits; reject, show "not usable" |
| A user calls the proxy to reach other services | The proxy only forwards to the one configured host and port, with the path under the plugin's prefix. No `..`, no redirects followed, no absolute URLs |
| The secret leaks | It only lives in the server's environment; the proxy strips the browser's `Authorization` and cookies and adds its own; never logged |
| A guest or a banned user uses the plugin | The proxy checks the account and the `access` setting on every request; role checks use the built-in server |
| The plugin is slow or stuck | Time-outs on requests (not on the event stream, which is cut when the browser leaves); a limit of open streams per plugin |
| A plugin runs commands as someone else | The user name comes from the session, never from the browser's request |
| Name clashes: a plugin id like `auth` | Ids are checked at start; the prefix `/api/plugins/` is separate from all existing routes |
| (A2 only) a framed page reads the user's session | `sandbox` without `allow-same-origin`, `postMessage` limited to declared capabilities, `Content-Security-Policy: sandbox` on plugin content as `internal/files` does |

## Recommendation

1. A prototype that needs no server change (A4): a panel that sends
   `!commands`. It shows the layout and the value early.
2. The proxy, the configuration and the identity header (B1, D, E).
3. The declarative renderer and the manifest from the plugin (A1, C1, G).
4. `!` completion from the manifest's commands (F).
5. Role gating (`staff`).
6. The sandboxed iframe (A2), only if a plugin needs it.

The work is split into issues in [../issues/plugins.md](../issues/plugins.md);
the manifest is in [plugin-manifest.md](plugin-manifest.md).

## Open questions

- Panel per server, or per channel? A music bot lives in one channel, so
  showing the button only there is tidier, but a panel that works from any
  channel is easier to find. Proposal: per server, with an optional channel
  list in the configuration.
- May guests use plugins by default? Proposal: no (`members`).
- Hide the bot's replies while the panel is open? Proposal: later, as an
  option.
- Should Jukebox adopt the contract? Decide after the first plugin works.
- Several plugins: a tab strip, or one button with a menu? Proposal: one
  header button; a menu when there is more than one.
