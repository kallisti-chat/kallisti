# Devlog

## 2026-10-14: Project start

Goal: a Mumble client with a Discord-like UI. Web first, desktop (Tauri) later.

Architecture for the web version:

- Browsers cannot open raw TCP/TLS or UDP sockets. A small proxy
  (`packages/proxy`) bridges a WebSocket to the TLS control channel of one
  fixed Mumble server. It is not an open proxy.
- The browser speaks the Mumble protocol itself (`packages/protocol`). The
  proxy only moves bytes. Because of this, the same protocol code can later
  run in a Tauri shell with a native TCP socket.
- Voice will go through UDPTunnel messages on the TCP channel, with Opus
  encode/decode by WebCodecs. This costs some latency against real UDP, but
  keeps the proxy simple. A WebRTC bridge is an option for later.
- `Mumble.proto` comes from mumble-voip/mumble `master`. The message order in
  the file is the same as the TCP message type IDs (0..26).

## Proxy notes

- `ws` needs an `error` listener on every server-side socket. Without it, one
  malformed frame (for example an unmasked client frame) crashes the process.
- All proxy users reach Mumble from the proxy IP. Murmur's autoban (10 failed
  attempts in 120 s by default) can ban the proxy and lock everyone out.
  Mitigations: a connection cap now, per-IP limits and a server-side autoban
  exception later.
- TLS ends at the proxy, so a browser cannot present its own Mumble client
  certificate. Users are unregistered for now. Registration needs per-user
  certificates stored at the proxy, or a different identity scheme.

## Web client notes

- The Mumble server does not echo your own text messages. The client adds
  them locally (`addOwnMessage`).
- Murmur kicks an older unregistered session with the same name from the
  same IP ("You connected to the server from another device"). Behind the
  proxy, all users share one IP, so this needs a look once there are
  real users.
- Stale users after a page reload were an artifact of agent-browser's CDP
  reload, which keeps the old socket open. A real in-page navigation closes
  the socket at once. In the worst case (dead peer), Murmur drops the user
  after 30 s without pings, and the proxy then closes both sides.
- Vite must not pre-bundle the workspace protocol package
  (`optimizeDeps.exclude`), or protocol edits do not reach the dev server.
- svelte-check needs the TypeScript 6 API. The web package has
  `typescript@6` next to the native `typescript@7`.

## Architecture direction (2026-10-14)

A deployment will be Murmur + our server + the web app. The proxy grows into
that server: it keeps the dumb byte bridge, and adds a separate API for text
channels, history, uploads and identity (TLS ends there, so it can hold
per-user certificates). Desktop Mumble users keep voice and in-room chat
only. The protocol and voice code must not depend on the sidecar.

Server language: Rust or Go are preferred. Decide when sidecar work starts.

## Voice notes

- Murmur 1.5 sends protobuf audio (`MumbleUDP.proto`, first byte 0 = Audio)
  to clients that announce 1.5, also inside UDPTunnel frames. Older servers
  use the legacy varint format. The client picks the format from the
  server's `Version` message.
- Voice over TCP is never lost or reordered, only late. Playback schedules
  each decoded chunk right after the previous one (in whole samples), and
  drops chunks when the queue is longer than 300 ms.
- The server does not echo our own voice. The speaking ring for ourselves
  comes from the capture side.
- The last frame of a transmission is sent with the terminator flag. The
  encoder keeps the order, so a FIFO of flags maps outputs to packets.
- Vite inlines the small capture worklet as a `data:` URL. A strict CSP must
  allow `data:` for worklets, or the build must emit it as a file.
- Testing: `agent-browser --args` breaks the browser launch here, so Chrome's
  fake media flags are not available. Instead, inject a fake microphone
  before connecting: replace `navigator.mediaDevices.getUserMedia` with a
  stream from an oscillator. `scripts/voicebot.ts` plays an Opus file into
  the server for playback tests.
- Encoder outputs are matched to inputs in order, not by timestamp
  (2026-10-16). Firefox shifts output timestamps by the Opus pre-skip
  (input 20000 µs, output 13500 µs) and holds the last packet back until
  `flush()`. So the terminator frame is flushed, and outputs after the last
  input are dropped: both Chrome and Firefox emit one extra packet on flush.
- An inactive worklet input has no channels; Firefox gives that for a
  digitally silent source. The capture worklet treats it as silence, so the
  voice detection still ends the spurt.
- `server/cmd/voicecheck` joins a channel and checks frame numbers and
  terminators per speaker. Firefox can be tested in the Playwright image with
  PulseAudio added (without a sound device its AudioContext stays
  suspended), with a fake microphone from an oscillator whose gain is
  switched on and off. In Lockdown Mode Safari has no AudioWorkletNode.
- CI (2026-10-16): `.github/workflows/voice.yml` runs `tests/voice/run.sh`
  in Chrome and Firefox (Linux, PulseAudio for a sound device) and Safari
  26.6 (macos-26 runner, safaridriver), driven by plain WebDriver. All pass.
  With the encoder fix reverted, Firefox fails (no terminators); Safari
  keeps input timestamps, as Chrome does.
- In this sandbox, `navigator.mediaDevices.enumerateDevices()` and a
  `devicechange` listener crash the headless Chrome tab (it lands on
  `about:blank`). The fake microphone script for tests must stub both.
- Svelte 5: an effect that reads `prefs.x`, where `prefs` is a derived
  object that is replaced on every change, re-runs on any settings change.
  This reopened the microphone on every slider step. Fix: derive the
  primitive (`$derived(prefs.inputDeviceId)`), which only notifies when the
  value changes.

## Server language: Go (2026-10-14)

The server (bridge now, sidecar later) moves to Go, in `server/`. Reasons:
fast to write network servers in, natural backpressure with blocking I/O,
and `pion` is a strong option if a WebRTC voice bridge is needed later.
WebSocket library: `github.com/coder/websocket`.
- Go bridge: `coder/websocket`'s `Close` waits up to 5 s for the peer's close
  frame. Shutdown therefore closes connections from a hook, while the
  connection's own reads keep running to receive that frame. Cancelling the
  read context instead drops the connection without a close frame.
- Bun replaces the `ws` package with its own WebSocket. It reports close code
  1000 where Node's `ws` shows the real code (1001). Use Node to check codes.

## Name: Kallisti (2026-10-14)

The product is called Kallisti (kallisti.chat). Packages use the `@kallisti/*`
scope, the Go module is `kallisti.chat/server`, and the client announces
itself as "Kallisti" to Mumble servers. The Gitea repository and the local
folder keep the old name "mumble-discord".

## Text channels (sidecar)

- The server now reads the Murmur-to-browser stream passively (it never
  changes it) to learn each connection's session and name from ServerSync
  and UserState. This replaces the rule "the server never parses the
  stream". The browser cannot fake this identity, but an unregistered name
  is only unique while its owner is online. Accounts will fix that.
- The JSON API uses WebSocket text frames on the same connection. Requests
  are rate limited (5/s, burst 10) and capped at 16 KB.
- SQLite runs with WAL and `synchronous=NORMAL`: a message insert happens in
  the sender's read loop, and a full sync on every insert would delay their
  voice.
- Playback limiter: Chrome's DynamicsCompressorNode adds make-up gain
  (1.14 at threshold -2 dB, ratio 20, knee 0), which made all quiet audio
  louder. A fixed gain of 1/1.14 after it cancels that. Measured with an
  OfflineAudioContext: full-scale tone at 200 % peaks at 0.85, levels below
  the threshold pass unchanged.

## Deployment (2026-10-14)

- One origin: the Go server serves the built web app, the bridge (WebSocket
  upgrades, the client uses `/ws`), uploads and files. The web app has a
  strict CSP; the capture worklet is therefore emitted as a file, not inlined
  as a data: URL.
- Tested with `podman compose` and `DOMAIN=localhost` (Caddy's internal CA):
  the app loads over HTTPS, and a wss client gets Murmur's handshake and the
  text channel list through Caddy. Not yet tested with a public domain.
- Bun's WebSocket ignores `rejectUnauthorized` (close code 1015 with a local
  CA). Use Node's `ws` for TLS checks against self-signed certificates.

## Desktop app (2026-10-14)

- Tauri v2. A local bridge in Rust (127.0.0.1, random port and secret, Origin
  check) lets the unchanged web client talk TLS to any Mumble server.
- Server certificates: trust on first use. The bridge only lets the page pin
  a fingerprint that it saw itself on a real connection.
- Tested: Rust integration tests (TLS echo, real Murmur) and the web client's
  desktop mode in a browser with a stubbed `__TAURI_INTERNALS__` (agent-browser
  `--init-script`). The app window itself could not be opened in the
  development sandbox; `tauri build` produces Kallisti.app.

### Native UDP voice

- OCB2-AES128 and CryptState are ports of Mumble's code. OCB2 has a
  deliberate countermeasure (XEX*) that changes an all-zero block, so test
  data must not be all zeros.
- The bridge reads Version and CryptSetup from the server's stream, pings
  over UDP, and sends voice over UDP while a good UDP packet came in the last
  12 s. Only whole frames go to the browser, so UDP voice never lands inside
  a TCP frame.
- Murmur sends voice to a client over UDP only after that client sent voice
  (not a ping) over UDP, and goes back to TCP when a UDPTunnel arrives over
  TCP. So when UDP stops, the bridge sends a ping in a UDPTunnel frame; a
  silent user would otherwise hear nothing.
- UserStats `udp_packets` is what the client reports about itself. Murmur's
  own count of decrypted UDP packets is `from_client.good`.
- Murmur's autoban blocks an IP after 10 connects in 120 s. The dev
  container runs with `MUMBLE_CONFIG_AUTOBANATTEMPTS=0` for the tests.

## Built-in Mumble server spike (2026-10-14)

- Grumble (Go Murmur) was checked briefly: little activity since 2023,
  announces 1.2.5, legacy UDP format only. Not a base.
- A minimal server in Go (`internal/mumbled`) passes the same bridge tests as
  Murmur, and the web client works through it with voice and text.
- Murmur (Server.cpp) sets a client's UDP flag only for UDP audio, not for
  pings, and clears it for a UDPTunnel over TCP. It requests a nonce resync
  when no packet decrypted for 5 s, at most every 5 s.
- Closing a `tls.Conn` waits for its writer (close_notify needs the write
  lock), so the server never closes a connection while it holds a lock.

## Built-in Mumble server (2026-10-14)

- `server/internal/mumbled` replaces Murmur: channels, accounts, Murmur's
  ACL model, moderation, whispers and listeners, Mumble clients before 1.5,
  comments and avatars, limits. The Kallisti server runs it by default; web
  clients join in-process (net.Pipe from the WebSocket bridge, with the real
  client IP), Kallisti accounts are Mumble registrations by their
  certificate hash. One container, no Murmur.
- Structure: `State` (channels, users, ACL evaluation, receivers) has no
  IO; handlers check permissions and call it under one lock. A change
  counter invalidates cached voice receivers, so no ACL evaluation runs per
  voice packet.
- The official client in podman (Xvfb, `packages/devtools/mumble-client`)
  tests every step: Mumble 1.5 from Debian trixie, 1.3.4 from bookworm.
  First-run dialogs: 1.5 reads a JSON settings file (`settings_version`
  and categories), 1.3 an ini file; certificates as base64 PKCS#12.
  xdotool drives menus (the ACL editor, "View Comment", "Listen To
  Channel"); there is no window manager, so tooltips do not show.
- Protocol details that cost time:
  - proto2 defaults: `ACL.ChanACL.inherited` and `ChanGroup.inherited`
    default to true, so a missing field means "inherited"; always send it,
    and do not filter on it when reading.
  - `BanList.BanEntry.address` and `mask` are required fields.
  - Legacy UDP positions are floats in native (little-endian) order.
  - A field in a UserState is a change for clients: send only set flags,
    or the official client logs "unmuted by the server" for new users.
  - Murmur switches a client to UDP voice only after UDP audio from it,
    not after a ping, and back to TCP on a UDPTunnel over TCP.
- Murmur's source (Messages.cpp, Server.cpp, ACL.cpp, Group.cpp) is the
  reference for every rule; reviews compared against it each step.

## 2026-10-15: Vite hides the CSP

Avatars (data: URLs) worked in every browser test and broke in
production: the Go server sends a CSP (`img-src 'self' blob:`), Vite
sends none. Browser tests of anything that loads resources (images,
media, workers, connections) must also run against the built app on the
Go server (`pnpm --filter @kallisti/web build`, then the server with
`WEB_DIR=apps/web/dist`).

## 2026-10-15: Chat features (Markdown, reactions, typing, previews)

- Murmur reads text messages longer than `textmessagelength` as XML (to
  check the length without images) and refuses invalid ones: `<br>` must
  be `<br/>` in messages with images (found with the Jukebox covers).
- Markdown runs on escaped text; code and links are set aside first (NUL
  placeholders, NUL stripped from input), so nothing inside them is
  formatted. Mentions for highlights, badges and notifications all use
  the written text, not the Markdown HTML.
- Reactions: emoji have several forms that look the same (with or without
  U+FE0F, joiners at the end). The server normalizes (FE0F after the older
  BMP symbols, nowhere else) and allows only symbols plus what sequences
  add (skin tones, FE0F, tags, joiners between parts). The picker uses the
  same form.
- Link previews: the server fetches (readers' IPs stay private). The
  dialer's Control function checks the resolved address of every
  connection (redirects, DNS rebinding), with keep-alive off (idle sockets
  to hostile hosts), a queue limit, one deadline for page and image, and a
  byte limit for cached images. Clients ask per message over the sidecar
  (read permission applies), not per URL over HTTP.

## 2026-10-15: Guest accounts and user ids

- Web guests get temporary accounts (cookie, no password, a Mumble
  registration on the built-in server); a password keeps one, a day without
  a visit removes it. Only with the built-in server: Kallisti cannot remove
  registrations on an external Murmur.
- Once accounts are deleted, names come back: everything that was keyed by
  name (message ownership, read marks, reactions) moved to the Mumble user
  id. SQLite reuses the highest INTEGER PRIMARY KEY after a delete, so the
  built-in server now keeps the highest user id ever given (mumble_meta)
  and never gives an id twice.
- The migration maps names to ids with the built-in server's mumble_users
  (same database, names.Key). Tested on a copy of the live database
  before the deploy (and a second start, which must change nothing).
- Cleanup and registration of web accounts take turns on one lock; the
  server does not register a web account whose account is gone, and does
  not remove the registration of a user who is online.

## 2026-10-15: RNNoise

- RNNoise is the Jitsi build (@jitsi/rnnoise-wasm, 112 KB .wasm) with our
  own 30-line wrapper in the capture worklet: the module only imports
  memory grow and memcpy, so the 1.9 MB Emscripten loaders are not needed.
- Chrome does not post a compiled WebAssembly.Module to an AudioWorklet
  (messageerror on the worklet side, nothing on the main thread). The main
  thread posts the bytes; the worklet compiles them itself.
- The CSP needs script-src 'wasm-unsafe-eval' (only WebAssembly, not
  eval). Found only with the built app: Vite sends no CSP.
- An AudioContext in a headless test tab only runs after a click; worklet
  experiments must come after one.
- DeepFilterNet3 comes from @workadventure/noise-suppression (only its DFN
  part ends up in dist: worklet, 14.6 MB engine, 8 MB model). It passes the
  compiled module in processorOptions, which works where port messages
  fail. Its dispose() does not free the filter state, so there is one node
  per AudioContext, kept and connected in or out. The Go server now gzips
  assets (the engine: 4 MB).

## Screen sharing (2026-10-16)

- WebCodecs over a WebSocket of its own (`/api/stream`): the server only
  forwards frames. VP8 first: a Chromium without proprietary codecs claimed
  to decode H.264 (`isConfigSupported` true) but failed on every frame.
- Picture and sound tracks from Chromium's tab capture stamp their frames on
  different clocks. Viewers that synced pictures to the sound clock showed a
  black picture (tests with a canvas and an oscillator shared one clock and
  passed). Both tracks are now put on the page's clock from their first
  frame, and viewers show pictures that seem far ahead at once.
- Hidden tabs (the usual case while sharing another window): frames come from
  MediaStreamTrackProcessor where it exists; a worker timer elsewhere (blob
  workers are blocked by the CSP: the worker is a file of its own).
- Mumble clients hear the stream from a hidden user that is not in the
  server's user list (nobody can message, move or kick it).

## Emoji picker (2026-10-17)

- Data from emojibase (names, keywords, GitHub shortcodes), cut to the
  Twemoji version and written to `public/emoji-data.json` at build time; the
  picker fetches it when it first opens (51 KB gzipped).
- emojibase's sequences are not always fully qualified (`🎓` with a
  selector), so they are not a source for the selector rules. Reactions use
  the fully qualified form from Unicode's `emoji-test.txt`: the selector
  after code points with Emoji_Presentation=No (a list that no longer
  grows), not before a skin tone. That rule reproduces all 3944 fully
  qualified sequences. The old rule (a selector after every older symbol)
  broke text-style emoji with skin tones; stored reactions move to the new
  form at start.
- Go's `unicode` tables are at Unicode 15: the emoji block counts as
  symbols as a whole, or Emoji 16/17 would be rejected.
- `@twemoji/parser` 17.0.2 does not find text-style emoji with skin tones
  (✌️, 🖐️, ☝️ …); 17.0.1 does. 👁️‍🗨️ shows as two images with it.

## Small gaps to Discord (2026-10-17)

From `meta/research/discord-comparison.md`.

- Spoiler attachments are marked by the file name (`SPOILER_` prefix), as
  in Discord: no new field on the server. Links inside `||spoilers||` get no
  preview card (client and server skip them), or the card would show them.
- Blocking is local to the browser, by lower-case name, like the voice
  mute. The server does not know the list, so a channel's unread dot and
  the mention count from before the login still include blocked users.
- `@here` reaches only who is online when it is sent: the server's summary
  at login counts `@everyone`, not `@here`, and the client counts `@here`
  only in live messages. The server stores which one a message has, and
  only for moderators; for others both stay plain text. An edit can take
  the ping away but not add one.
- Message links are `/channels/<chat>/<message>`; the server already sends
  the app for unknown paths. A jump into a chat waits for its first history
  page, or the jump gave up at once (also for search results).
- Search without words: an `OR` in the FTS condition made SQLite scan the
  whole table (0.1 ms became 13 ms at 300k rows), so the query has two forms.
- Chrome needs no service worker to install a web app any more; the
  manifest and icons are enough.

## Discord gaps, second round (2026-10-17)

- Built partly by parallel agents in git worktrees, merged at the end. The
  merges conflicted only where each branch added to the same lists (ready
  event fields, request types, the user menu); resolving meant keeping both.
- Light theme: a plain script in `<head>` (`public/theme.js`, allowed by
  `script-src 'self'`) sets the theme before the CSS paints; the app sets it
  again from the settings. The default stays dark.
- Channel notifications keep the old localStorage key: the parser reads
  the old list of muted ids as mutes "until turned off".
- Custom emoji images are blobs in SQLite, not files in UPLOAD_DIR: the
  cleanup of unattached uploads would delete them. Their ids are never used
  again, so the image URLs can be cached for ever. `:name:` images are added
  after sanitizing, like Twemoji, so message HTML cannot forge them.
- The bio is the Mumble user comment. Long comments come only as a hash;
  the web client fetches them when shown (user menu, settings), not all at
  once like avatars.
- Statuses: "invisible" does not fit Mumble (everyone sees who is
  connected). Away is sent by the client and not stored; the chosen status
  is stored for registered users.
- Time-outs mute in the built-in server and block chat in the sidecar.
  Guests are timed out by session, so reconnecting ends it for them; a ban
  is the tool for guests.

## Desktop app as a Kallisti client (2026-10-18)

- The desktop app's page has its own origin (tauri://localhost, or
  http(s)://tauri.localhost on Windows), so the server's session cookie is
  third-party there (WebKit blocks it). The app asks for the session token
  at login (`X-Kallisti-Session: token`) and sends it as
  `Authorization: Bearer`, and on the WebSocket as a subprotocol
  (`kallisti.session.<token>`; the server answers `kallisti`, as browsers
  require one): browsers cannot set WebSocket headers, and URLs end up in
  logs.
- The app origins are allowed on every server, and the cross-site guard
  skips them only without a cookie: other Tauri apps share the origins, and
  a local web server can pose as http://tauri.localhost.
- The app tells a Kallisti server by its exact answer to /api/me, without
  redirects. A timeout or offline is "cannot reach", not "Mumble server".
- Mumble clients can log in to Kallisti accounts with the account password
  (the built-in server asks the account store; no server password needed).

## Review before v0.9.0 (2026-10-18)

- An external review (GPT-6 Astra) found 30 points; 16 of the 17 bug
  claims were confirmed against the code. The worst: a voice target with
  linked channels kept a removed channel, and the next voice packet
  crashed the server (temporary channels go away by themselves, so any
  registered user could do it). Fixed and deployed at once.
- One ChatPane serves every chat: per-chat state (reply, edit, draft)
  must be reset or kept by chat id by hand; rows are keyed by message id,
  as reactions replace message objects.
- All per-address limits count an IPv6 /64 as one address.
- CI now runs the unit tests and type checks (test.yml).

## Invite-only channels (2026-10-18)

- Behaviour change: the built-in server now hides every channel a user
  may not Traverse, also outside private channels (Murmur shows them).
- One source of truth: a "members" group and ACL on the Mumble channel
  (for text channels, on their mirror). Kallisti checks text access
  through it, so Mumble clients and the web agree.
- Manual check (web): a member sees both channels and writes; a
  non-member sees no channel, message or search hit; a removed member
  loses the channel and its messages at once.

## Make admins (2026-10-18)

- Only SuperUser changes root's ACL through a Mumble client: root holds
  the roles, and an admin could otherwise drop other admins from the
  "admin" group, or the entry that gives the group Write. Admins can still
  read it, and use set-admin/set-moderator.

## Invite links (2026-10-18)

- Links are /invite/<token> paths: the server already answers every
  unknown path with the app, and the address stays through login or guest
  join (no reload), so no token has to be stored in the browser.
- Invites are keyed by the Mumble channel (a voice channel, or a text
  channel's mirror), like private channel access itself. The redeem
  answer names the text channel if there is one.
- A use is counted (one conditional UPDATE) before the member is added:
  no limit is overrun, but a failed add loses the use.
- Links of channels that stop being private are pruned on every access
  change (PrivateChanged); redeem also checks that the channel is still
  private, so pruning is only cleanup.

## Offline members (2026-10-18)

- Guest accounts are Mumble registrations too; only the store knows they
  are temporary (by name). The sidecar filters them, mumbled stays
  unaware of Kallisti accounts.
- The list is sent once with ready, then "member" deltas (add, rename,
  gone). Other lists (roles, private) are resent whole, but registrations
  come with every guest join, so a full resend would be wasteful.
- Bios are not in the list: Mumble comments can be large (HTML with
  inline images). The menu asks for an offline user's stored comment
  ("bio" request) when it opens.
- Stored DMs to offline users already worked (DirectChat needs only the
  user id); the menu just offers "Message". Role changes also worked
  (setRole checks the registration, not a session).

## Offline members: follow-ups (2026-10-18)

- "online" events tell every Kallisti client the session of each member
  that is connected, in any channel, from mumbled's state and not from
  the viewer's UserStates. Clients list members they do not see as online
  without a channel; only the id and session go out (statuses already go
  by session to everyone). Everyone gets the same set, so a viewer can
  infer "online, but somewhere I cannot see", as on Discord.
- Guests in hidden channels stay invisible: unregistered users have no
  id, and guest accounts are left out of the online set like out of the
  member list. Mumble clients see nothing new.
- A web account's first visit registers it before its session joins:
  mumbled now reports the registration after the join (and a Mumble
  client's self-registration after the state change), so the client gets
  "online" before "member" and never lists a new user as offline.
- Known gap: the Mumble UserState/UserRemove and the "online" event take
  different paths (mumbled's event queue), so a user who joins or leaves
  can show for a moment in Online without a channel.
- mumbled allows one session per registered user (another device kicks
  the old one), so a member has one session and one status.
- Making a guest account permanent changes only the store: auth tells the
  hub (OnPermanent), which finds the registration by name (accounts keep
  their registration's name, see identifyAccount).

## Music mode (2026-10-18)

- Stereo needs nothing from the protocol: the channel count is in each
  Opus packet (TOC byte), not in the Mumble header. A mono Opus decoder
  (old Mumble clients) mixes stereo packets down; our playback decodes
  with two channels already. The frame size stays 20 ms, so frame
  numbers do not change.
- WebCodecs' Opus `application` and `signal` members are newer than
  TypeScript's DOM types (typed locally in voice/opus.ts). Browsers
  without them ignore unknown dictionary members, so configure() does
  not fail; those browsers get the default (voip) tuning at 128 kbps.
- Chrome gives two channels only with echo cancellation off; music mode
  turns it off and asks for `channelCount: { ideal: 2 }`. Stereo is
  decided by the track's getSettings().channelCount (Safari may not say:
  then mono).
- The capture worklet sends planar frames (all left, then all right).
  The main thread derives the channel count from the frame length, because
  frames sent before the worklet gets `{ channels }` still have the old
  count. A new count or a music mode switch gets a new encoder at the next
  frame, also in the middle of a talk spurt; the old one is flushed first,
  or Chrome keeps its last packet and the encoder is never closed.
- Server: mumbled counts payload + 32 bytes of headers per packet against
  558 kbps. 128 kbps in 20 ms frames is about 143 kbps with headers;
  Opus VBR peaks stay well under the limit. No server change.

## Mass delete (2026-10-18)

- Direct messages are left out on purpose: staff do not read DMs, so they
  should not delete them unseen either.
- Web guests are registered (temporary accounts), so they go by user id
  like everyone else, offline too. Only unregistered Mumble guests go by
  session, and only for what they wrote in that connection (the sidecar's
  in-memory map, cleared when they leave). So the web client sends the
  delete before a kick or ban: the bridge handles text frames in its read
  loop, so the order holds.
- Known gap: unregistered guests on Mumble apps are not in that map (it
  is filled by web sends only and cleared on the web connection's close),
  so their messages cannot be found. Single deletes by staff still work.
- One "messages-deleted" per chat; a chat with one message still gets the
  old "message-deleted" (the desktop app bundles its web client, older
  builds do not know the new event).
- At most 1000 messages per request (newest first); a bigger spam wave
  needs another request.

## Guests read only (2026-10-18)

- The rule is in mumbled's `State.Permissions`: read-only guests lose
  TextMessage everywhere. So Mumble clients get PermissionDenied
  (TextMessage) for channel, tree and private texts, `MayWrite` (voice
  chats) follows, and the permission flush greys out their text box.
  `isStaff` now reads the ACLs alone (`aclPermissions`), else it would
  call `Permissions` again.
- Guest accounts are registered users: mumbled learns them at login
  (`Config.GuestAccount`, a store lookup) and keeps a flag on the user.
  Setting a password clears it (`Server.AccountPermanent`, next to the
  hub's) without a reconnect.
- The sidecar checks writing requests in one place (`readOnly`, next to
  `timedOut`) through `Voice.ReadOnly`, so mumbled is the only source of
  who is a guest. Upload tokens are refused; a token issued before the
  switch can still upload a file, but the message with it is refused.
- The setting is a row in `mumble_meta`; Murmur servers do not have it.
- Web: the composer gives way to a note with a button. A guest account
  opens the settings to set a password (and writes at once); a guest
  without an account goes back to the login screen.

## Connection info (2026-10-18)

- Ping answers come back with our timestamp, but the server's bridge also
  pings upstream (UnixMilli), and those answers reach the browser too. The
  client keeps its own timestamps (µs of `performance.now()`) and ignores
  the rest. Pings before sync only keep the connection: during the
  handshake the answer waits for the login and looked 20 ms slow.
- Pings now go every 5 s (Mumble's interval), not 15 s, so the 30 s window
  has about six samples.
- Late audio: a chunk that goes on the stream (frame numbers contiguous)
  but finds its queue run dry, or is dropped for a full queue. Our capture
  numbers frames without gaps across talk spurts, so the terminator must
  reset the expectation, else every new spurt counts as late. Decoder
  outputs come in order, so a queue beside the decoder (`queued`) carries
  each chunk's frame time and terminator flag; a bare terminator marks the
  last queued chunk. The frame time comes from the queue, not from the
  output's timestamp, which a decoder may count itself.
- Lost audio (frame number jumps up to 200 ms; longer is taken for a new
  spurt without terminator) only happens over the desktop app's UDP;
  over TCP it stays 0.
- Ping fields as Mumble: `tcp_ping_var` is the variance (clients show its
  square root), `tcp_packets` the samples behind the mean, good/late/lost
  totals since connect over all speakers.
- Levels: good below 100 ms and 2 %, fair below 250 ms and 8 %, else poor
  (the worse of both). The page cannot see whether the desktop bridge
  uses UDP, so it says "UDP or TCP".

## Connection stats (2026-10-18)

- Mumble numbers voice in 10 ms frames, so the frame number of a 20 ms
  packet goes up by 2. The web client takes it from the capture
  timestamp. Over TCP, mumbled counts by frame number: the step is the
  smallest one in a transmission, a gap is lost frames, an older number
  is late. A terminator, or a jump of more than 50 steps, starts a new
  transmission (the web client's numbers jump after a pause). Over TCP,
  "lost" means frames the client never sent, for example a throttled
  tab, not network loss.
- The web bridge sends its own keep-alive Pings (timestamp only)
  upstream, between the browser's. Murmur takes every ping field as it
  is. mumbled keeps only the fields a Ping has, otherwise each bridge
  ping would set the ping times to 0.
- The server cannot measure TCP round trips itself: a Mumble client
  takes every Ping it gets as the answer to its own. For web users the
  bridge's WebSocket ping could measure it, but nothing does that yet.
- UserStats follows Murmur's msgUserStats: version, address and
  certificates for oneself and for Register in root (not with
  stats_only); counters for the same channel; others need Enter (else
  PermissionDenied). Hidden users still get no answer. `from_client`
  is UDP (crypt) plus TCP (frame numbers). The Ping answer stays UDP
  only, because Mumble clients read it as their UDP stats.
- `Server.ConnectionStats()` is for the admin view. Sidecar overflows
  come in through `Config.TextOverflows` (`Hub.Overflows`), so mumbled
  does not import the sidecar.

## Connections view (2026-10-18)

- The window lives in the server, not per poll: every 10 s mumbled keeps
  a snapshot of each connection's counters (the last three). A poll and
  the log line count against the oldest one, so the window is 30 to
  40 s, and several admins polling share it.
- Mumble's crypt (and the server's TCP count) counts a late frame as
  good too; the web client's reports count it apart. The server takes
  the crypt's way for both: late or lost / (good + lost). For web
  clients that overstates a little (8.7 % instead of 8 %). Fewer than
  50 frames in the window give no percentage (one late frame in a quiet
  window looked like 50 %).
- In the crypt a late frame was counted lost first, so lost goes down
  again: within a window lost can shrink. Bad is late plus the change in
  lost (not below 0); only a smaller good means a client that started
  its counters over.
- Ping stays the client's: Mumble clients report the mean since they
  connected, so for them it moves slowly; the web client's is over 30 s.
- Log hysteresis: "connection poor" after 30 s poor, "connection good
  again" after 30 s good; fair resets both timers, so a connection near
  a limit stays quiet. Same levels as the web client's.
- No addresses in the table or the request: admins see them in
  Mumble's user information, a table on the screen shows them too
  easily.
- `sidecar.Connection` has the same fields as `mumbled.Connection`, so
  cmd/server converts it with a plain struct conversion.

## Slow mode (2026-10-18)

- Text channels only. Voice channel settings are Mumble channel state
  (Write in mumbled), and their chats are made lazily in the store; a
  slow mode there would need a second home for the setting. Direct chats
  have none.
- The turn is taken just before the message is stored, after all other
  checks: a refused text or a wrong reply costs no turn. Uploads go
  through the same `send`, so they count, but the file is uploaded
  before the refusal: the cleanup of unused uploads removes it.
- Last posts are in memory (keyed by user id, guests by session) and
  recorded also while slow mode is off: the composer counts from the
  newest own message it has loaded, so turning slow mode on must hold
  back who just posted on the server too. After a restart the server
  lets a post through that the composer still holds back (stricter,
  never looser). Clock skew: the composer counts in server times and
  caps the wait at the slow mode.
- mumbled asks before its time-out and permission checks: a timed out
  Mumble user in a slow channel can get the slow mode refusal first,
  and a refused text still takes the turn.
- Mumble clients: mumbled asks `Config.CheckChannelText` before it
  relays a text, outside its lock (the hub calls back into mumbled for
  the staff check). A refused text reaches nobody; the sender gets a
  PermissionDenied of type Text, which Mumble shows in its log as
  "Slow mode: wait 23 s". Only channels written to directly are asked
  (a tree message is not stored in a mirror, see mumbled textMsg).
- Web and Mumble share one turn per user and channel: a registered user
  waits on the web after writing from Mumble.

## Message rules (2026-10-18)

- One rule set for both issues (new-account limits, word and link
  filter): `internal/msgrules` is pure (rules, matching, `Check`);
  mumbled keeps the rules (JSON in a new `mumble_settings` table:
  `mumble_meta` holds integers), the registration times and the last
  message times, so a limited user has one rate over web and Mumble.
  The sidecar has one hook (`refused`, next to `readOnly`) that calls
  `Voice.CheckMessage`. With Murmur there are no rules.
- Registration time is a new `mumble_users.registered_at` (0 for rows
  from before, so old accounts are never new). A Kallisti account gets
  its Mumble registration on its first visit, so an account made before
  the update and first used after it counts as new once. A guest
  account that gets a password keeps the time of its first visit.
- Words: NFKC, without format characters (zero-width spaces) and
  fillers that count as letters (Hangul fillers, Arabic tatweel), Unicode
  case folding, without the accents of Latin, Greek and Cyrillic letters
  (not of others: a Japanese dakuten makes another letter). Then whole
  words (letters, digits, marks; `_` and `.` separate, so "spam" also
  blocks "spam.example"), phrases as word sequences. Han, kana, Thai,
  Lao, Khmer and Myanmar have no spaces between words: there an entry
  matches anywhere. Entries are words of letters and digits (with ' - . _
  between): "c++" would be the word "c" and block every "c". Homoglyphs
  (Cyrillic а for Latin a) and spaced letters ("s p a m") are not
  caught.
- Domains: every host-like token in the text (also bare "junk.example",
  markdown links, `user@host`, the ideographic full stop, percent-encoded
  hosts, which browsers decode) in its IDNA ASCII form, against each
  entry and its subdomains; a host that is no valid IDNA is compared as
  it is. Mumble texts count by their plain text: mumbled strips the tags
  (link targets too) before it sends a text on.
- Links for new accounts are what clients make links of: a scheme
  (`x://`) or `www.`. A bare domain is no link. @everyone/@here only
  pings for staff anyway; for limited users it is refused as noise on
  the web (Mumble texts never ping, and are not checked for it).
- The wait of limited users starts with a message the rules let
  through, also if the send fails later (channel rights, length).
- Refusals go to the author: on the web as an error in the composer, to
  Mumble clients as PermissionDenied (Text) with the reason, which their
  log shows. There is no moderator channel: blocked words and domains
  are a server log line (`message refused`, without the text).

## Registration captcha (2026-10-18)

- Stateless challenges: the token is salt and end, signed with an HMAC
  key made at start (a restart only voids open challenges). Only used
  ones are kept (at most 100 000, until they end), keyed by the decoded
  payload: Go's base64 decoder skips line breaks, so one payload has
  many spellings.
- The server checks the hash against the level now, not the level in
  the token: raising the level voids easier solutions found before.
- The check comes after the name and password checks (a taken name
  does not use up 1 to 2 s of work) and before bcrypt.
- Calibration: crypto.subtle in a worker did 340 000 SHA-256 a second
  in headless Chrome on an Apple M-series laptop (Node: 185 000).
  One `digest` at a time was half as fast in Node: the solver awaits
  batches of 512.
  Normal is 18 bits (0.8 s there, about 1.5 to 2 s on a mid laptop),
  hard 20 bits. Proof of work varies: 3 times the mean in 5 % of cases.
- crypto.subtle exists only in secure contexts: on plain http (not
  localhost) the check cannot run. Guests then join as classic guests,
  account creation shows the error.
- Classic web guests (no guest account) and Mumble clients do no check;
  they have the connection limits. The setting needs the built-in
  server (as guests read only).
- The worker is a separate file (`new Worker(new URL(...))`), so the
  desktop CSP needs no change: no `worker-src`, so `script-src 'self'`
  applies.

## Streaming views (2026-10-19)

- The server already lets a viewer watch several streams (viewers are a
  map per stream, one connection carries all). No server change.
- Grid and focus are one keyed list of tiles placed by CSS grid, not two
  lists: switching views or focus never remounts a canvas. A new canvas
  would stay blank until the next decoded frame, and a still screen
  sends few.
- The hidden strip only hides tiles (`display: none`): their streams
  still decode and draw. Simple; a hidden 2D draw is cheap next to the
  decode itself.
- Picture-in-picture is a `canvas.captureStream()` in a video element
  that is never in the page (Chromium takes that). Firefox has no
  Picture-in-Picture API: no button there. The Document PiP API would
  need the canvas moved out of the page; not used.
- Stream pictures with sound wait for the sound clock in an animation
  frame loop, which stops in a hidden tab, so PiP froze while you were
  in another tab. In a hidden tab pictures now show at once (up to the
  0.15 s sound lead out of sync).
- Headless Chromium enters PiP (the promise resolves) but
  did not enter full screen; full screen was not checked there.

## Emoji resize (2026-10-19)

- Still images are scaled in the browser to fit 96×96 (no server
  change: Go's content sniffing already knows WebP). The image is scaled
  once to a working canvas with a short side of at most 96 × 8 (the most
  zoom), so dragging the crop redraws a small canvas, not the photo.
- The output is PNG or WebP, the smaller one. `toBlob("image/webp", 1)`
  is lossless WebP in Chromium, and it gives a PNG where the browser has
  no WebP encoder, so the type of the blob is checked.
- Animation detection walks the GIF blocks (a 0x2C byte can be in the
  image data) and also finds APNG (`acTL` before `IDAT`): a canvas would
  drop those frames too.
- Small images (≤ 96 px, no crop) are uploaded as they are; re-encoding
  them gains nothing.
- agent-browser's `mouse wheel` sends no DOM wheel events (checked with
  a test page), so wheel zoom was not checked there. Drag, keys and the
  slider were.

## Web Push (2026-10-19)

- New dependency: `github.com/SherClockHolmes/webpush-go` (RFC 8291
  encryption and RFC 8292 VAPID; by hand it is easy to get wrong). It
  pulls in `golang-jwt/jwt/v5`. Its `Subscriber` adds `mailto:` to
  anything that is not `https:`: `PUSH_SUBJECT=mailto:x` must lose the
  prefix first.
- Who gets a push is decided in the sidecar (`push.go`): `pings` (name
  mention, reply without no_ping, @everyone, direct message; not the
  author) and `wantsPush` (may read the chat, not connected, not in do
  not disturb). @here never pushes: it is for who is there, and pushes
  only go to who is not.
- "Not connected" is `Voice.Online()` (web and Mumble clients): an open
  tab notifies by itself, so there are no double notifications. A phone
  whose PWA is in the background but still connected gets neither.
- Offline users have no session, so access needs new questions to the
  voice server: `UserSees` and `UserMayRead` build a registered user by
  id (in no channel). Groups and roles by id count; a certificate hash
  or tokens in ACLs do not.
- A subscription belongs to the login session (foreign key, cascade):
  logout, "log out everywhere", password change and expiry end it
  without extra code. The client subscribes again for the new session
  on the next start (`syncPush`).
- The server posts only to the browsers' push services (allowlist of
  hosts); otherwise any account could make it post to internal
  addresses.
- Channel settings and blocks stay in the browser. Only the channels set
  to "Nothing" and the blocked names go to the server, per subscription
  (per device): mutes let mentions through anyway, and pushes are only
  for pings. Without the blocks a blocked user could still ping a closed
  phone.
- Most messages ping nobody: without "@", a reply, @everyone or a direct
  chat, the decision makes no queries.
- Rate: one push per user and chat per minute (in memory), plus the
  `Topic` header, so a push service keeps only the newest per chat.
- The service worker is a second Vite entry (`src/sw.ts` to `/sw.js`),
  registered only while push is on, with no fetch handler: no caching,
  the update check works as before. It must stay one file (no shared
  chunks): only it imports `lib/pushNotice.ts`.
- The WebWorker TypeScript lib clashes with DOM: sw.ts declares the few
  worker types it uses.

## Channel recording (2026-10-19)

- No re-encoding: the voice path hands each packet (copied, the buffers are
  reused) to the recording's goroutine through a channel of 1024; when it
  is full, packets are dropped and counted (`dropped` in recording.json).
  Voice never waits for the disk.
- Alignment: every track starts at the recording's start and ends at its
  stop. Within a burst, Mumble's sequence numbers (10 ms frames) place the
  packets, so network jitter does not move them and lost packets become
  silence. A new burst (after a terminator, a pause over 1 s, or sequence
  numbers more than 2 s off the clock) starts where the arrival time says.
  A burst that starts while the last packet still plays goes after it.
- Silence is explicit packets, not granule gaps: CELT frames `TOC 0xff
  0xfe`. The CELT silence flag is the first symbol in the range coder, so
  the same two bytes are silence at 20, 10, 5 and 2.5 ms; gaps are filled
  with 20 ms frames, then shorter ones (all Opus durations are multiples of
  2.5 ms). Mixing these CELT frames with SILK speech is valid Opus.
- Stereo (music mode) is kept: a track starts mono and its OpusHead is
  rewritten in place (page 0, CRC again, same size) at the first stereo
  packet. Mono packets in a stereo stream decode fine; the other way round
  would be downmixed.
- Pre-skip is 312 (libopus's encoder delay at 48 kHz) on every track, so
  they stay aligned to each other.
- Checked with ffmpeg: real libopus packets with gaps gave two 8.000 s
  tracks (mono and stereo) that decode without errors, with the sound
  exactly where it was spoken (silencedetect).
- Only what a user says to its own channel (target 0) is recorded, if it
  may speak there (not muted, suppressed, or without Speak). Whispers,
  shouts, linked channels and screen share sound are not.
- Downloads: the sidecar gives staff a one-time token (1 min, as stream
  tokens); `/api/recordings/{id}?token=` streams a stored zip (Opus does
  not shrink) and checks again that the session may record. The token is
  in the URL because a download is a plain link.
- A crash leaves a recording without recording.json: it is not listed, and
  removed after the keep period plus the maximum length.

## Flaky mumbled tests (2026-10-19)

- The timeouts were all `superUser` dials ("no frame of type 5", the
  ServerSync), not the registration: helpers call `t.Helper()`, so the
  failure points at the test line. The ServerSync wait (3 s) covers the
  argon2id check of SuperUser's password: 0.3 s under -race, seconds on
  a busy machine. Reproduced with 24 parallel test binaries
  (GOMAXPROCS=2): 46 failures in 960 runs.
- TestWebPasswordChecksShareTheLockout had the same cause: its ten
  failed checks took more than 12 s, so the lockout limiter refilled.
- Fix: tests store a cheap argon2id hash (`setAdminPassword`); Check
  takes the parameters from the hash. 0 failures in the same stress.

## Camera streams (2026-10-19)

- A user has one screen share and one camera at most. The hub keys
  streams by an id: the session for a screen share, the session with
  bit 31 set for a camera (`stream.ID`). Frames up carry a camera flag
  (bit 2); frames down carry the id, so viewers and the channel list
  need no other change. Cameras have no sound. There is no server-wide
  limit on the number of streams; one upload limit (1 MiB/s) covers
  both streams of a user. The server sends it in "ready".
- Watching a camera is opt-in like a screen share (a camera button next
  to LIVE). Simple; Discord shows cameras at once in its voice grid.
- Quality while live: 480p/720p/1080p and 15/30/60 fps, default 720p at
  30 fps (about 4 Mbit/s; screen shares were 1080p at 15 fps, so small
  text is less sharp by default now). The frame loop compares the wanted
  encoder settings with the current ones on each frame. A new size: a
  new encoder (it starts with a keyframe) and a new "stream" config to
  the viewers, who keep their decoder (it takes the new size at the
  keyframe; a new decoder missed that keyframe while it was set up, and
  the server asks for keyframes at most once a second). A new bitrate or
  frame rate only: the same encoder is configured again. Checked in
  headless Chromium: 720p to 1080p60 (screen) and 720p to 480p and back
  (camera) while watched, without an end.
- Alone, a stream may use 80% of the upload limit; screen and camera
  together share it 65/35.
- Camera switch (a phone's front and back): the current camera closes
  before the next opens, because phones open one camera at a time.
- Headless Chromium crashed its renderer on `enumerateDevices`, also
  with the fake media flags. The browser check used an init script that
  replaces getUserMedia, getDisplayMedia and enumerateDevices with
  canvas streams.
- Watching no longer needs getDisplayMedia (it needs decoders only), so
  phones without screen capture can watch too.

## Streaming from OBS (2026-10-19)

- WHIP with pion (new dependency). The stream is an "ingest" in the
  stream hub: viewers see a screen share of the user's voice session; the
  user's own stream connection neither feeds nor ends it. It starts with
  the first keyframe, whose SPS gives the size and the codec name.
- Viewers get the same Annex B chunks the browser encoder sends (no
  decoder description); the server puts the last SPS and PPS in front of
  keyframes that come without them. B-frames are not reordered: OBS should
  send none.
- Each offer gets its own pion API with exactly the offered H.264 and Opus
  parameters, so any H.264 profile matches (pion compares profile ids).
- One UDP port for all sessions (pion's UDP mux), IPv4 only: FFmpeg's WHIP
  client takes only the first candidate, and it was an IPv6 one. FFmpeg
  also read the end of a chunked answer as an error (now Content-Length).
- Picture and sound timestamps come from RTP, each started at its first
  packet's arrival (no RTCP sender reports): in step up to the difference
  in network delay.
- Checked with FFmpeg 9's WHIP muxer (as a stand-in for OBS): the
  forwarded H.264 decodes with FFmpeg without errors. The headless
  Chromium here claims H.264 support and fails on every frame, so the
  picture in a browser was not checked; it now shows the H.264 message.
- Opening Settings in headless Chromium leaves the page (about:blank),
  also before this change: the Settings section was not checked there.

## Playback buffer (2026-10-19)

- Setting "Playback buffer": Low 60 ms (default), Medium 120 ms, High
  250 ms. The maximum queue is the start delay + 240 ms (60 ms gave the
  old 300 ms), so a larger buffer is not cut away.
- Music gets 500 ms (or the setting, if larger): who is in a listen-only
  channel (only speakers can speak there; `listenOnlySessions`), and who
  sends music. Music: per 1 s of audio (durations from the Opus TOC), a
  stereo packet or more than 96 kbps (our speech is 40 kbps mono, Mumble's high preset 72, music
  mode 128 kbps). 3 music seconds in a row switch to music, 10 speech
  seconds back; silence counts for neither, so a jukebox stays music
  between tracks. Counters only, in place: it runs for each packet.
- Each speaker's timing is chosen when its stream starts or stalls, so a
  change never cuts audio mid-stream.
- The connection info offers the next level ("Use Medium (120 ms)")
  from 2 % late audio (the "good" limit). A button that sets the level
  was simpler than opening the settings at the right place. The old late
  audio stays in the 30 s window, so after a change it shows the new
  level instead of a second offer.
- The desktop app's bridge forwards voice to the same web playback, so
  all of this applies there too.

## Offline avatars without 404s (2026-10-19)

- mumble_users.picture caches "the texture is an image Avatars serves",
  so ready can flag members without reading textures. Old databases get
  it filled in the same transaction as the new column (a migration may
  now have a fill function).
- A member "picture" event goes out on every texture save by a
  registered user, also when the flag stays the same: the web only keeps
  the set of ids, and the URL is revalidated by ETag anyway.

## Linux desktop audio (2026-10-19)

- Report: the AppImage had no voice in or out. Reproduced in podman
  (Ubuntu 22.04 arm64, the CI's WebKitGTK 2.50.4) with the real app
  binary and AppImage, Xvfb and PulseAudio null devices. Three causes:
  - No GStreamer plugins in the AppImage. WebKitGTK plays WebAudio,
    records the microphone and does WebCodecs Opus with them. Without:
    "autoaudiosink not found" (the AudioContext stays suspended), no
    capture devices, and isConfigSupported says no to Opus. Fix:
    `bundleMediaFramework` (+10 MB on arm64; the AppImage carries what
    the runner has installed, so CI installs plugins-base and -good;
    pulsesink/pulsesrc are in -good and also serve PipeWire).
  - getUserMedia: NotAllowedError until the app answers WebKitGTK's
    permission-request. media.rs allows UserMediaPermissionRequest for
    our own origins only. enable-media-stream is on by default in 2.50,
    set anyway for older ones. No DeviceInfoPermissionRequest came for
    enumerateDevices (labels come after getUserMedia, as elsewhere).
  - WebCodecs may lack Opus (no plugins, older WebKitGTK): codec.ts
    falls back to libopus-wasm (MIT, 440 kB chunk, loaded only then;
    the CSP already allowed wasm-unsafe-eval). The choice is made once
    per page from isConfigSupported. Encoder and decoder have one small
    interface (planar floats in, packets out); calls made while the
    codec loads wait in order.
- WebKitGTK 2.50 has AudioWorklet, AudioContext at 48 kHz and WebCodecs
  audio; it lists no audio outputs and has no setSinkId, so the output
  picker stays hidden there.
- KALLISTI_DEVTOOLS=1 turns on the web inspector on Linux.

## Native notifications (2026-10-19)

- Report: "desktop notifications not supported" in the Linux AppImage.
  WebKitGTK asks the app for a NotificationPermissionRequest (we only
  answered UserMedia: refused), WKWebView (macOS) has no web Notification
  API, WebView2 (Windows) has one.
- The desktop app now uses tauri-plugin-notification 2.7 (system
  notifications on all three). notify.ts picks a notifier: the plugin in
  the app, the Notification API in browsers, none without it. The
  plugin's permission is read once at start (on desktop it always says
  granted; a block in the system settings is not visible to us).
- The plugin also replaces window.Notification with a shim that calls
  it, so web code that uses the constructor still works in the app, but
  without onclick.
- Clicks: 2.7 reports "tap" on desktop too (macOS and Windows: a thread
  per notification waits for it; Windows only while the toast is on
  screen, not from the Action Center; Linux: D-Bus ActionInvoked). Rust
  brings the window forward (unminimize, show, set_focus); the page maps
  the notification id to its handler and opens the chat. Ids start at a
  random number, so old notifications after a reload do not open the
  wrong chat.
- media.rs also allows NotificationPermissionRequest for our own page
  (with the shim it should not come).
- In the app, a visible window behind other windows counts as not seen:
  notifications for the open chat need the window focus (chatSeen).
- Verified on macOS (release .app, no signing): permission granted and
  the notification sent through the plugin without error. Seeing the
  banner and clicking it was not checked here; Linux (cargo check in
  podman) and Windows by users.

## Paste in the desktop app (2026-10-19)

- Report: pasting an image to attach it works in browsers, not in the
  Linux AppImage. Reproduced in podman (Ubuntu 22.04 arm64, WebKitGTK
  2.50.4, Xvfb; `xclip -t image/png`, `xdotool key ctrl+v`), in a plain
  WebKitGTK view and in the real app binary (tauri://localhost) with a
  probe page.
- WebKitGTK gives the paste event nothing for an image: types [], files
  0, items []. A text paste gives text/plain as usual. In the same paste
  handler `navigator.clipboard.read()` gives the image (image/png, no
  prompt, no permission request, also with wry's default settings).
  With only image/jpeg on the clipboard it gives no types.
- Fix in the web client, no Rust: in the desktop app, a paste without
  files and without text/plain reads the clipboard API and attaches the
  first image as "pasted.png". Checked in the app binary: a 7x5 PNG
  arrives as a File that decodes; a text paste does not read.
- macOS and Windows give pasted images as files, so the read runs there
  only for a paste with neither files nor text (an empty clipboard, for
  example). WebView2 may then ask for clipboard access, WKWebView may show its
  "Paste" callout (not tested).

## Music buffer from the first packet (2026-10-19)

- A user heard ~5 s of rough audio in the jukebox channel after joining:
  music detection needed 3 s, and a new buffer applies only at the next
  stall. Stereo (the jukebox sends 128 kbps stereo; speech is mono) now
  counts as music from the first packet, so the stream starts with the
  music buffer. The bitrate rule keeps its 3 s.
- The "larger playback buffer" hint counts only late audio of speech: the
  music buffer covers music speakers, the setting does not change them.

## Audio after start (2026-10-19)

- Setup: local built-in server, 25 registered Mumble users with avatars
  (random-noise PNGs, 50-120 KB, 2 MB in all) in "General", one of them
  talking without pause (20 ms Opus frames), 400 messages in "general"
  (every 4th with a 70 KB JPEG). Listener: headless Chromium over raw
  CDP, a guest who joins General at once; a hook counted per Mumble
  message type and JSON event the bytes and arrival times on the socket,
  and sampled playback's good/late/lost every 100 ms. Slow link: a TCP
  proxy that gives all connections one bottleneck (8 or 3 Mbit/s down,
  20 ms each way); Chromium's network throttling was not used. CPU x4:
  CDP CPU throttling. 15 s per run, fresh profile (no cache).
- Before, 8 Mbit/s: in the first 2.3 s the socket carried 2.0 MB of
  avatar blobs (RequestBlob answers for all online users), then voice;
  JSON (ready, one history page, voice chat) was 12 KB. Voice reached
  the listener 2.2 s after the join, as a burst: 96 late (dropped)
  chunks, then smooth. 3 Mbit/s: 5.6 s and 270 late chunks, the
  reported "jerky for about 5 s, smooth once the history is in" (the
  history page itself comes early: it goes out beside the blobs).
  Unthrottled with CPU x4: 0.6 s and 19 late. So the cause was head-of-
  line blocking in the per-client queue, not main-thread work.
- After (a, below), same runs: voice 0.08 s (8 Mbit/s) and 0.15 s
  (3 Mbit/s) after the join, 0 late chunks in 10 s (one run: 1), also
  with CPU x4 and x6. The avatars still cross the same link over HTTP,
  but no longer queue ahead of voice. Long tasks stayed at 140-170 ms
  (CPU x1) and 370-640 ms (x4-x6) in the first 10 s without late audio,
  so main-thread work (d) and deferring history or previews (b) were not
  done: the numbers do not show them.
- (a) Online registered users' pictures come from /api/avatars/<name>
  ?v=<texture hash> (from UserState) instead of RequestBlob; the hash
  makes them cacheable for good. For online users the endpoint serves
  the live texture: the database gets it a moment after the UserState
  with the new hash, and a client that asked in between would keep the
  old picture (or a cached 404). ready's `avatars` says the server
  serves them (built-in server without a password: as for offline
  members, a private server keeps them private). The client asks for
  textures only after ready, or 3 s after the sync without one (no
  Kallisti server); guests and servers without the flag still use
  RequestBlob.
- (c) mumbled sends queued voice before other queued frames. Without
  HTTP avatars (private server): voice starts after 0.7 s instead of
  2.2 s (8 Mbit/s), late chunks 97 -> 47; at 3 Mbit/s 271 -> 225 with
  gaps up to 1 s. Most of the backlog is below the queue (the pipe to the
  bridge, the bridge's 32 KB writes, the TCP send buffer), so this only
  helps a little; Murmur upstreams get nothing from it. Voice can now
  come before ServerSync, so the web client plays voice only once
  synced (local mutes and volumes are set then).
- Not measured: WebKitGTK (Chromium only), the desktop app's UDP voice.

## Linux notifications: icon and button (2026-10-19)

- Report (AppImage): a broken icon and an empty button in the
  notification. Reproduced in podman (Ubuntu 22.04 arm64, Xvfb) with the
  plugin's D-Bus call: app_name and app_icon "kallisti-desktop" (the exe
  name; no such theme icon in an AppImage), actions ["default", ""].
  lxqt-notificationd shows an empty button for it, xfce4-notifyd a
  missing-image icon; dunst and notification-daemon show no icon.
- tauri-plugin-notification 2.7 adds the "default" action with an empty
  label itself, so on Linux the app now sends notifications over D-Bus
  (notify.rs, zbus like the plugin): app_name "Kallisti", app_icon a
  file:// URI of icons/128x128.png written to the cache dir at start,
  actions ["default", "Open"]. Same connection for sending and signals
  (dunst), kept open (GNOME). macOS and Windows still use the plugin.
- The page calls the app's `notify` command on all desktops; a click
  comes as the "notification-action" event (the plugin's onAction
  shape), after Rust brought the window forward.
- Checked with the release binary and a probe page using nativeNotifier
  in lxqt-notificationd, xfce4-notifyd, notification-daemon and dunst:
  icon shown, an "Open" button in LXQt and xfce (notification-daemon
  and dunst show none), a click restores the minimized window and runs
  the page's handler. LXQt keeps the notification open after the click.

## DM context menu (2026-10-19)

- Right-click, long press or the context-menu key on a direct chat in
  the sidebar: Profile, Mark as read (only when unread), Mute (the
  channel durations), Pin to top / Unpin, Close conversation. No
  delete: the other person keeps the chat. ChannelMenu got a `direct`
  mode instead of a second menu component (same styles, same mute block).
- Closed and pinned chats are kept on the device (localStorage
  `directChats`, per server), like channel mutes. Closed stores the
  newest message id at close time: a newer message lists the chat
  again, and so does opening it (user menu "Message", a link). Pure
  logic in lib/directList.ts. Server-side (per account, synced) would
  need a new table: read_marks_by_user only holds a message id per chat,
  so it cannot store "closed" or a pin order. A later option.
- Mutes reuse channelPrefs with the DM's chat id. A muted DM has no
  desktop notification, no sound, no badge or bold and no tab title
  count. Only mutes "until I turn it back on" go to the push "quiet"
  list: the client sends the list only while a tab is open, so a timed
  mute would stay quiet on the server after it ended. Notification levels are not offered:
  every DM message is for us, so only mute makes sense.
- Closing marks the chat read: else its unread count stays in the tab
  title with no row to show it.
- Gap: Ctrl+K does not find a closed chat whose partner is offline (an
  online one shows as a user).

## Desktop app hangs (2026-10-19)

- Report: the Linux AppImage hung, for good or until "WebProcess didn't
  exit as expected after the UI process connection was closed", when the
  user joined the Music channel and when the user Settings opened.
- Cause: WebKitGTK starts and stops its GStreamer audio output
  (AudioDestinationGStreamer) with webkitGstSetElementStateSynchronously,
  which waits on the pipeline's bus on the page's main thread without a
  time limit. When a context gets its first AudioWorklet module, WebKit
  restarts a running output (BaseAudioContext::workletIsReady ->
  restartRendering: a stop, then a start). Our context starts at login,
  and the capture worklet came when the microphone opened first: at a
  voice join, or when the Settings open (level meter). Both reports came
  at that moment.
- Repro: podman, Ubuntu 24.04 with PipeWire 1.0.5 and WirePlumber, Xvfb,
  the real arm64 AppImage (its WebKitGTK 2.50.4 and GStreamer 1.20), the
  built-in server and a stereo 128 kbps sender in the container. A
  script acts as a Bluetooth headset that changes profile when a
  microphone opens: the output sink goes away and a new one comes 20 ms
  later. Settings: 4 hangs in 10 runs; voice join with the music
  stream: 2 in 8. Stacks: the main thread in gst_bus_timed_pop_filtered
  (from the restart), the AudioWorklet thread (the new render thread) in
  appsrc's push, the sink waiting on its clock. Closing the window then
  prints the reported message. A PipeWire without a session manager
  hangs the same wait already at the first start.
- Fix: in the Linux app the context is suspended at once, gets the
  capture module, then resumes. suspend() marks it "suspended by script"
  before WebKit's lazy start, so the output starts once, after the
  module, and the restart has nothing to do. Same repro: 0 hangs in 10
  and 8 runs (and 6 with the decoder change below); the GStreamer log has one start at login and none at the
  voice join or in the Settings. Browsers keep the old order: Safari on
  iOS keeps a resume() outside a tap suspended.
- Left: the start at login and the stop at logout still wait without a
  limit; a sound server that does not answer then still hangs the app
  (a WebKitGTK bug).
- Memory: WebCodecs decoding in WebKitGTK (2.50.4 and 2.52.6) keeps
  about 16 KB per stereo 20 ms frame in AudioData.copyTo to f32-planar;
  a copy in the data's own format (f32) crashes the web process ("free():
  invalid pointer"). Decoding without copyTo and the encoder stay flat.
  With the music stream: +0.8 MB/s, 3 GB an hour. The Linux app now
  decodes with libopus (wasm): flat at about 230 MB over 90 s of music,
  with sound on the output. Encoding stays on WebCodecs.
- Headless Chromium dying on Settings: agent-browser starts Chrome with
  --single-process. There, enumerateDevices or a devicechange listener
  kill the whole browser on any page (SIGTRAP; getUserMedia: SIGSEGV),
  and Settings use both when they open. A normal headless Chrome runs the
  same calls, so the cause is not in our code. Tests in agent-browser
  must stub all three (getUserMedia, enumerateDevices and
  mediaDevices.addEventListener); then Settings open.

## Long message collapse (2026-10-19)

- A message collapses when its written text has more than 20 lines; a
  line longer than 120 characters counts as more lines (lib/collapse).
  Code blocks count with their lines. A full 2000-character line is 17
  lines: it stays open on a wide screen (on a phone it is taller).
- Mumble HTML has no source text: its lines come from htmlToLines, and
  its measured height also counts (over 400 px, again after each image
  load): htmlToLines folds blank lines, and images have no lines.
- Collapsed: 12 lines, faded with mask-image, then "Show more". The text
  is only clipped, so screen readers read all of it; focus (Tab) in it
  expands it. Expanding keeps the scroll place; "Show less" scrolls the
  line back into view.
- Search results and pins are not collapsed: they show plain text with
  the newlines folded, so they stay short.

## Join and leave sounds (2026-10-19)

- Sounds for others joining or leaving our voice channel, our own join
  (also a move into another channel) and leave, mute and deafen. Our own
  notes, synthesized like the mention ping (ping.ts now has a shared
  `playNotes`): rising for joins and unmuting, falling for leaves and
  muting, three notes for our own steps, lower ones for deafening.
- Pure logic in lib/sounds.ts: `voiceSound(prev, next)` compares two
  snapshots of the state and gives at most one sound. No previous
  snapshot (the sync at login: Client is mounted per connection) gives
  none. Deafen changes win over everything; while deafened nothing else
  plays. Others only count when they enter or leave our channel.
- Burst limit: one sound per 500 ms (`allowSound`), for all sounds.
  After a reconnect none for about 3 s: the server restart brings
  everyone back at once, and the client goes back into its old channel
  and mute state (which would give a join and a mute sound).
- Setting "Sounds" (on by default) with a volume slider in Settings.
  Played on the chosen output device (setSinkId where it exists).
- Checked in headless Chromium against a local server, with
  createOscillator wrapped to log the notes: join, mute, another user's
  login into the lobby (silent), join and leave, deafen and undeafen.
- Gap: a reconnect was not tested by hand.

## Who reads a private channel (2026-10-19)

- No new event: the "private" list (sent again on every access, member
  and role change) now has the members also for the channel's members.
  Others who see the channel (by an ACL from a Mumble client) still get
  it without members; the client then shows the normal list, as it
  cannot tell "no members" from "not told".
- Staff come from the roles list, which everyone already gets. Each user
  shows once: a staff member who is also a member is under Staff.
- Groups keep online users first, offline ones faded below, instead of
  four headings (Members online/offline, Staff online/offline).

## Word replacements (2026-10-19)

- Part of the message rules (`Rules.Replacements`, the same JSON
  setting and admin tab). `msgrules.Replace` runs before `Check` in
  `checkMessageLocked`, so `CheckMessage` now gives the text to post. The
  sidecar puts it into the request (send, edit, upload caption); the
  server stores it, so the author sees what everyone sees. Mumble texts:
  unescape, replace, escape again; the web hooks get the replaced text.
- For everyone, staff too: it is for fun, not moderation. Blocks and
  limits stay staff-exempt. A replacement can contain a blocked word;
  then the message is refused (the admin's choice).
- One match: one replacement at random (`math/rand/v2`; tests inject
  `pick`). Case: all capitals (two letters or more) give capitals, a
  capital first letter gives one; else the replacement as written.
- Too long after the replacement: refused, not cut (the web composer
  gets its text back; Mumble: TextTooLong). Cutting would lose the end
  without a word.
- No injection: replacements may only have letters, digits, marks,
  emoji (So, skin tones, ZWJ, VS16), spaces and ' . , ! ? - (checked on
  the server and in the tab). Nothing that formats (* _ ~ ` | >), links
  (: /), mentions (@ #) or is HTML. The message format has no escapes,
  so a check is simpler than escaping. Nothing is replaced in code,
  links, @mentions, #channels and :emoji: names, and a phrase matches
  only with separators (spaces . - _) between its words, so "**nice**
  ass" keeps its markup ("**nice** behind" with an entry for "ass").
- Matching (also for blocked words, which gain all of this): each
  character with its combining marks is folded on its own (NFKC, marks
  off Latin, Greek and Cyrillic letters and off what is no letter or
  digit, case folding, lookalikes), with a byte map back to the
  original, so matches replace the original text. Lookalikes: a small
  table of lower-case letters, applied after case folding so both cases
  match (Cyrillic а е о р с у х and в н к м т as small capitals, Greek
  α ο ν ρ and others, Latin small capitals, dotless i); full-width and
  mathematical letters come from NFKC. Greek Μ and Ζ stay (their small
  letters look like nothing Latin).
  Single letters with only spaces, dots, dashes or underscores between
  them are one word ("a s s", "a.s.s"); a single letter next to a longer
  word stays a word ("a spam"). A letter three times or more also
  matches fewer of it ("asss", "spaaam"); twice does not ("too" is not
  "to", "good" is not "god"). Whole words only: "class", "assistant",
  "passage", "bass" stay.
- Left: Russian or Greek words that fold to a Latin entry match ("сор"
  is "cop"); ß folds to ss ("aß" is "ass", as before); joined letters can
  make a word of initials ("A. S. S."); Hangul as separate jamo and
  half-width kana with separate marks do not match; the rules' entries
  are folded again for each message (fast enough for 500).

## Join and leave sounds dropped (2026-10-20)

- Users found the join and leave sounds annoying; they are gone (ours and
  others'). Mute and deafen sounds stay: they confirm one's own action.

## Stream playout buffer (2026-10-20)

- Report: a shared YouTube tab (with sound) is "mostly fine, sometimes
  choppy" for a viewer in America (server in Helsinki, 120-180 ms RTT: a
  lost TCP segment stalls the stream for 200-400 ms).
- Setup: `tests/perf/stream.sh` (see tests/perf/README.md): a streamer
  in headless Chromium shares a canvas with high motion (720p30, the
  default quality: 3.8 Mbit/s VP8), one viewer direct and one behind
  linkproxy, which now has jitter (`-jitter`, `-jitter-chance` for
  spikes). 20 s per run, one run each, a Mac. Scenarios (viewer link):
  far = 10 Mbit/s, 150 ms RTT, spikes of 200-400 ms; jitter = 8 Mbit/s,
  0-200 ms always; tight = 5 Mbit/s, 150 ms RTT, spikes of 150-300 ms;
  spiky = 10 Mbit/s, 150 ms RTT, spikes of 400-800 ms; slow = 3 Mbit/s
  (below the bitrate). local: two viewers without a proxy.
- Before, the viewer behind the proxy (freezes: gaps over 200 ms between
  drawn pictures, count and total; latency: send to draw, median):

  | Run | Freezes | Other | Latency ms |
  | --- | --- | --- | --- |
  | local, sound / silent | 0 / 0 | first picture after 7.8 / 7.5 s | 207 / 4 |
  | far, sound / silent | 0 / 4 (1261 ms) | | 580 / 94 |
  | jitter, sound / silent | 0 / 1 (240 ms) | | 446 / 166 |
  | tight, sound / silent | 0 / 6 (1539 ms) | 2 sound packets late | 396 / 108 |
  | spiky, sound / silent | 5 (1968 ms) / 1 (441 ms) | sound: 41 pictures not drawn, 5 keyframe asks | 961 / 93 |
  | slow, sound / silent | 5 (7058 ms) / 2 (496 ms) | 227 / 30 lost, keyframe wait 8 s / 1 s | 1417 / 1754 |

- Causes, from these numbers: (1) a stream without sound had no buffer
  at all (pictures drawn on decode), so every spike froze it. With sound,
  the 0.15 s lead plus the sound's own delay (pictures were 0.2-0.4 s
  behind their capture) covered spikes up to 400 ms, not 800. (2) After
  an 800 ms stall the backlog came as a burst; more than 10 frames in
  the decoder made the viewer drop to the next keyframe and ask for one
  (5 asks, 41 pictures not drawn). The sound clock's new start after 5
  late packets did not show here (0 jumps), only on the slow link (37).
  (3) Keyframe waits: a new viewer's keyframe often came before its
  decoder was set up, and the decoder's own request fell in the server's
  one-a-second limit and was lost: the viewer waited for the periodic
  keyframe (every 8 s). A viewer whose queue was full asked at once,
  and that keyframe found the queue still full: again up to 8 s. (4) The
  streamer was not the cause: the encoder queue stayed at 0, no frame
  was skipped, keyframes were 83-95 KB (the upload limit is 1 MiB/s with
  2 MiB burst), and the direct viewer lost nothing. Not measured on a
  slow streamer machine.
- (a) Playout (`lib/stream/playout.ts`): picture and sound on one clock,
  the audio context's with sound, else the page's (silent streams get the
  buffer too). The first item, picture or sound, sets it: it plays a lead
  of 0.5 s after it came. A late item raises the lead in steps of 0.25 s
  (up to 1.5 s) and plays at once: a pause where the stall was, not a
  skip; sound and pictures already scheduled keep their times, so both
  pause at the same place. After 60 s without late items the lead goes
  down by 5 ms a second (sound buffers play 0.5 % faster meanwhile, or
  they would overlap), while the slowest item of that time still had
  over 0.5 s to spare. Still late at 1.5 s: dropped;
  late for 1 s: the clock starts over (a delay that stays); an item over
  1 s beyond the lead: starts over (the clock jumped, or a backlog
  drained).
- Pictures wait encoded and go to the decoder 0.1 s before their time:
  decoded VideoFrames are large (1.4 MB at 720p) and hardware decoders
  stall when many are held. Late pictures are still decoded (the next
  ones need them) but not drawn. At most 240 encoded pictures wait; the
  decoder may hold 30 (was 10, which bursts after a stall passed).
- One loop draws all watched streams: animation frames, and in a hidden
  tab the worker timer (no animation frames there), so picture-in-
  picture stays in sync with the sound; before, a hidden tab drew
  pictures at once.
- (b) Server: a keyframe request within the limit is kept and sent with
  the first frame after it (the limit is now 0.5 s, was 1 s). A viewer
  that was full asks for its keyframe when its queue has room again
  (at most a quarter full), not at once, and at most every 2 s: without
  that limit one slow viewer brought keyframes for all twice a second
  (11 requests in 17 s on the slow link; 4 with it). The periodic
  keyframe stays at 8 s: with these two, recovery did not need it (0
  requests from the server in all runs but slow).
- After (same runs; a/v: a picture is drawn 0-12 ms after its sound is
  scheduled, p5 to p95; the first picture now comes at least the lead
  later):

  | Run | Freezes | Other | Latency ms |
  | --- | --- | --- | --- |
  | local, sound / silent | 0 / 0 | first picture after 0.6 / 0.6 s | 526 / 509 |
  | far, sound / silent | 0 / 0 | | 585 / 660 |
  | jitter, sound / silent | 0 / 0 | | 660 / 668 |
  | tight, sound / silent | 0 / 0 | | 584 / 952 |
  | spiky, sound / silent | 2 (567 ms) / 1 (275 ms) | 0 not drawn, 0 asks | 1078 / 910 |
  | slow, sound / silent | 6 (5282 ms) / 5 (3801 ms) | 98 / 41 lost, longest wait 2.7 / 1.5 s | 3371 / 3521 |

  An 800 ms spike still pauses (by what the lead does not cover yet);
  then the lead is higher and later ones pass.
- (c) Adaptive bitrate: not done. Only a link below the stream's bitrate
  (slow) still stutters, and a buffer cannot help there: the server's
  queue fills, drops until a keyframe, and the cycle repeats. With sound
  a little better, silent worse (probably because the keyframe comes
  sooner, so the overload starts again sooner); latency 3.5 s. The fix
  is a lower bitrate: the server would tell the streamer from viewer
  drops, but one encoder serves all viewers, so one slow viewer lowers
  the quality for all (simulcast would avoid that). A follow-up.
- Gaps: Chromium only (no Firefox, Safari, desktop app); hidden tabs and
  picture-in-picture not checked headless; the slow lowering of the lead
  is only unit-tested (runs are 20 s); one run per scenario; latency is
  now 0.5-1.1 s (was near 0 without sound, 0.2-1 s with), as wanted for
  one-way watching; an audio context that stays suspended would hold
  the pictures of a stream with sound (as before). Pictures follow the sound's schedule, not what
  is heard: the output latency (large on Bluetooth) puts them early, as
  before. A new start of the clock that moves it earlier (rare: a
  backlog drained by over 1 s) lets sound already scheduled overlap the
  new for up to a second.

## Review fixes: client (2026-10-20)

- Stream playout: after a late item the lead went down by the drift of all
  the time since, also with no item in between. A still screen share
  (one picture after 5 min) lost its next picture. Now the lead goes down
  only after an on-time item was seen since the late one, and by at most
  1 s of drift per item. Recording the late item's own slack (as the
  review proposed) was not done: it is below one step by design, so the
  lead would never go down again.
- Voice codecs: a failure (load, call, or the codec's error callback) is
  for good. Queued calls (with their samples) are dropped, later ones
  ignored, the error is reported once. Capture drops the failed
  encoder's pending inputs and sends nothing until the next talk spurt,
  which tries a new encoder (not at once: a codec that cannot load would
  fail in a loop).
- Playback drops voice of a session until the client set its local mute
  and volume (setUserGain, from the UserState and the per-name settings).
  Before, voice that overtook a muted user's UserState after a
  reconnect played at full volume until the mute came. The first packets
  of a new speaker (until the next effect run) are lost.
- Avatars: the server serves (and flags as pictures) only images of at
  most 1024 px a side, online and offline. It reads the header only
  (image.DecodeConfig; WebP from golang.org/x/image). Gap: databases that
  flagged a larger picture before keep the flag until the next save; the
  client then gets a 404 (cached 5 min).

## Review fixes: push (2026-10-20)

- Sending: 16 workers and a queue of 1024 pushes (was a goroutine per
  push that waited for one of 16 slots, so a slow push service piled
  them up). The one-per-minute collapse comes before the queue; a full
  queue drops the push (counted, logged on the first and every 100th).
  On stop, sends on their way are cancelled and the queue is dropped.
- Banned users get no pushes: offline access (`UserSees`, `UserMayRead`)
  and the new `UserBanned` check the user's certificate against the ban
  list. Direct messages need no access check, so the hub asks
  `UserBanned` itself. An address ban cannot count for a user who is not
  connected (no address); a registration without a certificate is only
  banned by address, so it still gets pushes.
- Client: subscribing (start sync, prefs, the switch) runs one at a
  time. Turning off does not wait in that line (a stuck
  `serviceWorker.ready` must not keep it from the logout): it counts up,
  and subscribing asked for before it stops, or unsubscribes, sends the
  DELETE and unregisters what it made. Subscribing asked for later waits
  for the turning off. A `ready` that never comes now holds all later
  subscribing in that tab (before: only that one call).

## Review fixes: streams and recording (2026-10-20)

- WHIP: Pion's H.264 depacketizer appends FU-A fragments until an end
  fragment, also across pictures, so unfinished starts grew memory without
  bound. A wrapper drops an unfinished unit at each start and one over
  1 MB (the hub's frame limit); its picture counts as lost (keyframe
  asked for). Pictures over 1 MB are lost too.
- WHIP: the stream key is checked again under the lock EndAccount takes
  when the session is reserved: a key revoked while the offer came in
  (slowly) starts nothing. A track's end ends the session (bad packets
  are skipped; before, they ended the reader), and a live stream without
  pictures for 10 s ends (it froze).
- Recording: at most 50 tracks (each an open file until the end); later
  speakers are not recorded and are named in recording.json
  (`unrecorded`). A write error stops the recording ("write error", told
  like the limits). The size limit counts the page still buffered, and
  the silence that ends the tracks: it goes a second at a time on all
  tracks and stops at the limit, so the recording ends earlier ("size
  limit"). A stop that finishes while a newer recording runs is not told
  ("Recording stopped" would be false).
- Mumble port: an Accept error (out of file descriptors) no longer ends
  the accept loop: it waits (5 ms, doubling to 1 s) and tries again;
  only a closed listener ends it.

## Avatar flags and image size limits (2026-10-20)

- The picture flags of registered users are checked again once at start
  (`pictures_checked` in mumble_meta, one transaction): avatars flagged
  before the 1024 px check, or broken ones, lose the flag (no more 404s).
- imagesize: one header-only check (image.DecodeConfig) for all images.
  Inline images (chat, link previews, custom emoji) have at most
  16384 px a side and 2^26 pixels (256 MiB decoded): panoramas and
  "50 MP" phone photos (8192x6144) pass, pixel bombs do not. Avatars
  stay at 1024 px. Rare JPEGs Go cannot read (arithmetic coding,
  12 bit) count as broken.
- Mumble texts: an oversized picture is dropped, its place gets
  "[image too large]"; the text stays. Pictures whose header does not
  decode are dropped without a note (they are no real images).
- Uploads: an oversized image is kept, but stored as
  application/octet-stream, so it is a download and the web app shows a
  file card. Friendlier than a refusal: the file still gets through.
  The check reads the stored file, as a JPEG's size can come after
  the first 512 bytes. Uploads and emoji from before are not checked.
- Link previews: an oversized or broken image leaves the card without
  one. Custom emoji: refused (413, broken: 415).

## Channel moderators (2026-10-20)

- One source of truth: a "channelmod" group in the Mumble channel (a voice
  channel, or a text channel's mirror) and one ACL entry for it, here and
  in subchannels: Traverse, Enter (they see a private channel), MuteDeafen
  and Move. No Write: with it they could edit the ACL and appoint
  moderators. Kallisti's rights ask the same group (ModeratesIn); Mumble
  groups inherit, so subchannels count. The entry goes with the last
  moderator.
- Rights: delete and pin messages, a text channel's topic and slow mode
  (they are exempt from it there), server mute, move out, the members and
  invite links of a private channel (not making it private or public),
  and channel time-outs. Voice channel chats have no topic or slow mode.
- The voice "kick" is a disconnect: a move to root (the lobby). A server
  kick or ban is server-wide, so it stays with staff. Mass delete stays
  with staff too: it spans all channels.
- Never over staff, and not over the same channel's other moderators:
  native Mumble mutes and moves check this too (State.shields), as Mumble's
  ACLs alone would allow it. Since then, nobody without staff rights
  (also a temporary channel's maker, who has Write there) mutes or moves
  staff. Staff act on channel moderators; nobody acts on SuperUser.
- A moderator must see the channel: a private subchannel stays closed to
  the parent's moderators (Mumble groups inherit, but the subchannel
  denies Traverse). Only staff change a "channelmod" group in a Mumble ACL
  edit (others keep the old one): a temporary channel's maker has Write
  there and could appoint himself. A user timed out in a channel cannot
  lift the suppress there.
- Deletes and pins look at the chat, not at the author: a channel's
  moderators also delete staff messages there (as Discord's Manage
  Messages). Their mutes are server mutes (Murmur's), so they stay when
  the user leaves the channel.
- Appointed by admins and moderators in the channel menu (Moderators…),
  with the members dialog (registered users). Changes go out with the
  roles event (channelModerators: each channel's own moderators, only of
  channels the user sees); every ACL edit now sends it, as groups of any
  channel can hold moderators. The member list tags them (an outlined
  "Mod") only in their channels.
- Channel time-outs share the time-out map and timers (a key with the
  channel) and are saved in mumble_channel_timeouts (no foreign key:
  temporary channels are not saved; time-outs of channels that are gone
  are skipped at the start). They take Speak, Whisper and TextMessage in
  the channel and below (State.Permissions), so Mumble clients see the
  rights, users there are suppressed, and the sidecar's MayWrite and
  MaySpeak follow. Text channels now also need TextMessage in their mirror
  to write. They come in the timeouts event as channelTimeouts (older
  clients ignore them). A new request type (channel-time-out), so an
  older server refuses it instead of timing out server-wide.
- Channel moderators of private channels pick new members from the
  members list (registered users but guest accounts): Mumble's user list
  needs Register in root.
- Known gaps: the web walks the channel tree for inherited moderators and
  ignores a group's Remove entries from Mumble ACL edits. A Mumble client
  timed out in a channel cannot send private texts to users in that
  channel either (Murmur checks TextMessage in the recipient's channel).

## Voice subchannels of channel moderators (2026-10-20)

- Channel moderators make permanent voice channels in the voice channels
  they moderate, at any depth. Below the moderated channel they rename,
  reorder, move and delete them (also temporary ones there). The
  moderated channel itself stays as it is, and nothing leaves or enters
  the subtree. Their moderator rights cover the new channels, as groups
  inherit.
- Not ACL entries: State.Permissions adds the rights (modPermissions):
  MakeChannel where they moderate, Write where they also moderate the
  parent. Mumble clients see them in PermissionQuery and use their own
  menus; the web follows the same bits. Write from the ACLs would let
  them edit the ACL, so ACL edits (mayEditACL) and keepWrite ask the ACLs
  only, as do listen-only and Mumble's user lookup (QueryUsers); a
  channel made with a moderator's MakeChannel gives its maker no admin
  group or Write entry. Sees asks the ACLs' Traverse (Permissions asks
  Sees for moderators). Mumble clients show the ACL tab in subchannels;
  the server refuses it.
- Moves need Write and the new parent's MakeChannel both from the ACLs or
  both a moderator's (mayMove): no channel of their temporary channel
  goes in, none of theirs out. A move or delete with a moderator's Write
  needs it in every channel below too (modWritesAll): a private
  subchannel they do not see stays where it is.
- Private subchannels: yes. create-private-voice takes the parent
  (voice); a moderator who makes one is its first member, else it would
  not see it (private channels are closed to a parent's moderators).
  Making a channel private or public stays with staff.
- Text channel mirrors and everything below their parent get none of
  this (State.Mirror).
- The rights are added to the ACLs' (not filtered by them): an ACL deny
  for "channelmod" does not take them away.
- Limits (State.modLimit, like Murmur's channelcountlimit and
  channelnestinglimit): a channel made with a moderator's MakeChannel is
  refused if the moderated channel has 50 channels below it, or if it
  would be more than 5 levels below it. The moderated channel is the
  topmost of the moderator's above the parent, the one it may not edit;
  with nested moderated channels the outer one counts. Mumble clients
  get ChannelCountLimit / NestingLimit with the reason, the web the same
  text as a manage error. Staff are not limited. A moderator's move
  within the subtree is refused (NestingLimit) if the moved channel's
  deepest one would be more than 5 levels below; the count does not
  change, as nothing enters the subtree. The web has no channel moves.
- Known gaps: two mirror checks (isMirrorLocked: the parent and its children,
  State.Mirror: any depth).

## Text categories (2026-10-20)

- One level, like Discord. A store table (categories: name, a unique
  name key as Mumble compares names, position, Mumble channel) and a
  category column on text channels. Old databases: every channel is
  uncategorized; the mirrors stay where they are until moved.
- Mumble side: a channel per category under "Text channels", after the
  uncategorized mirrors, with its channels' mirrors as children. One
  sync call (SyncTextTree) places everything: a mirror moves when its
  category changes. A category denies TextMessage in itself only; the
  parent's deny of Speak, Write and MakeChannel reaches it and its
  mirrors. Mumble clients can enter a category channel but not write
  there.
- Category moderators are the same "channelmod" group, on the category's
  channel. Mumble groups inherit, so they moderate all its channels and
  a channel time-out given there holds in all of them, with no new code.
  A private channel in a category stays closed to the category's
  moderators unless they are members (as with private subchannels).
- Rights: staff create, rename, order and delete categories, and move
  channels between them (the edit dialog's category field). A category's
  moderators create (not private) channels in it, and rename, order and
  delete the channels in it that they see. They may not bring back a
  deleted channel's history: CreateChannel revives a hidden channel only
  for staff (it may have been private).
- Deleting a category moves its channels to the top, without a category
  (not refused). Mirrors move to the parent before the category channel
  goes, and clients learn the move first: otherwise a Mumble client drops
  the mirror with the users in it.
- One mirror check (inTextTree, State.Mirror): the mirrors' parent and
  everything below it, at any depth. Categories and their mirrors get no
  voice rules (listen-only, recording) and no channel moderator
  subchannel rights (MakeChannel, Write).
- Ordering a category's channels puts them into the places they had in
  the global order; the others keep theirs.
- Clients get the categories with ready and every channels event, only
  those with a channel the user reads and those it moderates (staff:
  all). Collapsed categories are kept per server on the device
  (localStorage "collapsedCategories"); a collapsed category still shows
  the open channel and unread ones. Alt+Up/Down and the quick switcher
  follow the sidebar's order; the switcher shows the category's name.
- Manual check (headless browser): created a category, a channel in it,
  moved a channel into it, collapsed it (kept after a reload), appointed
  a moderator who then created, moved and edited channels only there,
  and deleted the category (its channels moved to the top).
- Known gaps: no drag and drop (menu and dialog only). No private
  categories in the app (the server does not refuse set-private on a
  category's channel, but that is untested). A category's moderators
  can learn that a channel name is taken (also a private or deleted channel's) from the create error.

## Threads (2026-10-20)

- A thread is a chat of its own: a hidden channels row (its id is the
  thread's id) plus a threads row (text channel, start message, name,
  maker, archived). History, send, uploads, reactions, pins, typing,
  read marks and search go through the usual chat paths. The sidecar's
  chat has a parent; may(), mayModerate (the parent's mirror) and the
  push check use the parent text channel, so private channels, channel
  and category moderators, read-only guests, time-outs, slow mode and
  message rules hold as in the channel. One level: only messages of text
  channels start threads (no threads in voice chats, direct chats or
  threads).
- Old databases get the new tables at the start. Store queries that mean
  "text channel" use one predicate (textOnly), so channel changes never
  touch a thread's row.
- Rights: who may write in the channel starts a thread (it takes a slow
  mode turn there, and the message rules check the name). Its maker
  renames and archives it; staff and the channel's moderators also delete
  it. Slow mode: the channel's time, with turns per thread.
- Deleting the start message keeps the thread ("The original message was
  deleted" in its pane). Deleting (hiding) the channel hides its threads
  too; they come back if staff bring the channel back. Deleting a thread
  removes its messages and files, but keeps the hidden row (renamed):
  SQLite would give a deleted row's id again, and clients keep ids.
- Following: the maker, those who write in a thread and those it
  mentions or answers (not @everyone) follow it; the follow event goes
  out before the message. Followed threads count as unread, and their
  summary comes with ready; others show in the list without unread
  marks. Muting and notification levels are the client's per chat id, as
  for channels. Push notifications: mentions, as in channels.
- Thread settings follow the channel: a thread without its own mute and
  level takes its channel's (withParents, used for unread marks,
  notifications, the title count and the mention sound). Its menu sets an
  own setting, stored whole with its level (so "Only @mentions" or no
  mute also hold against the channel); "Same as the channel" forgets it.
  Push: the server resolves it, as the client does not know every thread
  (new or archived ones while no tab is open). The note has the thread's
  channel; a device's quiet channel keeps its threads quiet unless the
  device lists the thread as loud (a chat with a level of its own).
- Archive: a minute timer (main.go) archives threads without a message
  for the staff setting (1 h, 1 d, 3 d (default), 1 w; store_meta). A new
  message opens a thread again. Archived threads stay readable and
  searchable, in the channel's thread list (paged by id).
- Wire: ready and channels events carry the active threads of channels
  the user reads; ready has follows and threadArchive. Events: thread
  (made or changed: name, replies, last reply, archived, start quote),
  thread-deleted, follows, archived-threads, thread-archive. Messages
  carry the thread they started; search results carry the threads of
  their messages.
- Web: "Thread" in a message's actions asks for a name (the start of the
  message). The thread opens in the right column on desktop (one ChatPane
  in compact mode), full screen on phones. Under the start message a box
  says "N replies · last reply 5 min ago". Active threads are indented
  under their channel in the sidebar; the channel header's thread button
  lists active and archived threads. The quick switcher finds active
  threads; search results say "#channel › thread". The thread's "⋯" menu
  and a right-click in the sidebar: follow, archive, rename, delete,
  mute and notifications.
- Mumble clients never see threads: no mirror, no relay, no history on
  entering the channel.
- Manual check (headless browser): made a thread from a message (name
  from the text), wrote in it with a mention; the other user got it as
  unread with a mention badge, opened it from the reply box, could not
  archive or delete it. Archived it (gone from the sidebar, in the thread
  list), a new message opened it again. Quick switcher and search opened
  it and jumped to the message; phone width shows it full screen.
  Deleting it closed it for both.
- Slow mode: the channel's composer counts the threads we started there
  (their creation time) as turns, as the server does.
- Links to chats the client does not know (maybe an archived thread):
  get-thread asks for it by id, with the history's rights check; the
  thread opens when it comes, at the message. Refused (no thread, or not
  for us: the same answer) says "You cannot open this message here". A
  reconnect mounts a new Client: the place now has the open thread, which
  opens again (asked for if archived).
- Manual check (headless browser): a thread's menu shows "Same as the
  channel" only with an own setting. A link to a message in an archived
  thread opened the thread after a page load; a server restart kept the
  archived thread open. Not checked by hand: channel mute in the sidebar
  (agent-browser sends no right-click) and the slow mode countdown (needs
  staff); both have unit tests.
- Known gaps: no pins panel inside a thread (pinning works). Both panes
  share the upload error. Each channels event and ready queries the
  threads per connection. Thread settings stored before this change with
  a mute only count as own settings in the client, but are not loud for
  push. A thread started and then deleted within the slow mode time no
  longer counts in the countdown (the server still refuses the post).
  Push subscriptions made before this change keep threads of "nothing"
  channels quiet until the app sends its settings again (at its start).

- Known gaps: no pins panel inside a thread (pinning works). A thread's
  mute and notification level do not follow its channel's. Opening a
  link to an archived thread the client does not know yet does nothing;
  a reconnect closes an open archived thread. The composer's slow mode
  countdown does not know that starting a thread took a turn (the server
  refuses the next post). Both panes share the upload error. Each
  channels event and ready queries the threads per connection.

## Media dimensions (2026-10-20)

- Uploads keep width, height (pixels, as shown) and duration (ms) in
  three new columns (0: unknown). internal/mediasize reads them from
  headers only: pictures with image.DecodeConfig (same limits as
  imagesize; JPEG Exif orientation 5 to 8 swaps them, as browsers do),
  MP4/M4A from moov (mvhd duration/timescale, the first tkhd with a
  size; a matrix turned by about 90 or 270 degrees swaps), WebM from
  Segment → Info (Duration × TimecodeScale) and Tracks (PixelWidth/Height,
  DisplayWidth/Height in pixels or as aspect ratio). It walks with
  ReaderAt and one budget of 4096 box/element headers per file, so moov
  at the end of a large file costs a few reads and hostile input cannot
  loop. Truncation tests and fuzz targets (FuzzMP4, FuzzWebM) cover it.
  MP3, Ogg, WAV and FLAC get nothing (yet).
- Old uploads: measured once at start (store.MeasureUploads, flag
  upload_sizes in store_meta), with the type as served (old
  octet-stream uploads with a media name). Files are read with no
  connection held (SQLite has one); results and flag go in one
  transaction. A file missing then is not measured again.
- The upload check for oversized images now uses the same read: an
  inline image type without a size becomes a download.
- Wire: upload objects carry width/height/duration, link previews
  imageWidth/imageHeight (when known; the card stays 80x80).
- Web: imageBox scales the size into 400x300 (the CSS maximum); the img
  gets that width and `aspect-ratio: auto w / h`, so the loaded
  picture's own ratio still wins (object-fit: contain, so a wrong size,
  say Exif in PNG/WebP, does not squash it). Videos get `aspect-ratio: w / h`;
  checked headless that the box is the same before and after loading.
  Players show the server's length until the metadata loads.
- Known gaps: WebM DisplayUnit 1/2 (cm, inches) and PixelCrop are
  ignored (pixel size used); the first MP4 track with a tkhd size wins
  (no hdlr check); JPEG fill bytes before APP1 end the Exif scan (then
  "not turned").

## Media previews (2026-10-20)

- Optional ffmpeg (internal/thumbs). At start: MEDIA_PREVIEWS=0 → off;
  else FFMPEG_PATH or `ffmpeg` on PATH, and `ffmpeg -encoders` picks WebP
  (libwebp) or JPEG (mjpeg). The log says which. No ffmpeg: as before.
  Homebrew's ffmpeg has no libwebp (JPEG path), Alpine's has it.
- Videos (MP4, WebM): a frame at 1 s (the first frame if shorter than 2 s
  or of unknown length; `-ss` past the end makes ffmpeg fail), at most
  480 px. Pictures (PNG, JPEG, WebP) over 1600 px a side or over 1 MB:
  1000 px. GIFs: none (they keep their animation). PNG/WebP only with
  WebP output (yuva420p keeps transparency); JPEG output would lose it. A
  picture's preview not smaller than the picture is dropped. Nothing
  above 2^26 pixels (ffmpeg's memory).
- JPEG Exif orientation: ffmpeg gets `-noautorotate` and the turn as a
  filter (transpose/flip, from mediasize.ExifOrientation), so the preview
  looks as browsers show the original. Videos keep ffmpeg's autorotate
  (display matrix). The real-ffmpeg test checks both.
- Safety: a separate process (exec.CommandContext, 20 s, killed after,
  WaitDelay 1 s), behind `nice -n 10` if there is one, env only PATH,
  `-nostdin -protocol_whitelist file -threads 1`, input `file:<path>`
  with the demuxer forced by the stored type (mov, matroska, png_pipe,
  jpeg_pipe, webp_pipe): no HLS/concat playlist can pull in other files.
  stderr kept to 1 KB, output at most 8 MB. At most 2 processes (one
  semaphore for the queue workers and the backfill); queue of 64, full →
  dropped (the backfill gets it at the next start).
- Files: ffmpeg writes into os.TempDir (the /tmp tmpfs in the read-only
  container), the result is copied into the upload dir as `.preview-*`
  and renamed to `<key>.preview` (rename alone fails across file
  systems). Never in the upload request: queued after the upload is
  stored (browser uploads and Mumble pictures).
- Store: uploads.preview: "" not tried, the type when made, "-" when the
  file gives none (thumbs.ErrFailed: ffmpeg failed, timed out, or wrote
  nothing usable). Other errors (temp dir, disk, shutdown) leave "" and
  the next start tries again; jobs stopped by the shutdown are not
  reported.
- Serving: `/files/<key>/<name>?preview`, the same lookup as the upload
  (unguessable key, plainKey), only if preview is an image type; nosniff,
  sandbox CSP, immutable cache. Deleting an upload (message deleted,
  unused) deletes the preview; a preview done after the upload was
  deleted is removed (SetUploadPreview finds no row).
- Wire: upload objects get `preview` (the URL) once made. When it is made
  after the message was sent, the hub sends the message again as
  `message-edited` (no `edited` mark), so clients swap it in. Chosen over
  clients probing for the URL (404s, and the client would have to know
  the rules).
- Old uploads: a background backfill at start (attached pictures and
  videos with preview "", the newest first, one at a time through the
  same limit). Chosen over making them on request: no 404-then-retry in
  the client, and the rate is bounded by the semaphore.
- Web: lib/media chatSrc (the preview in the chat, the original in the
  lightbox and links; a failing preview falls back to the original) and
  videoPreload (poster → preload="none"). The video src stays the same
  when a poster arrives later, so a playing video is not reloaded. Checked
  headless against a local server (homebrew ffmpeg, JPEG): a 2400x1200
  JPEG shows `?preview` in the chat and the original in the lightbox; a
  3 s MP4 gets a poster, preload none, length 0:03, the same box before
  and after play.
- Docker: `apk add ffmpeg` (ffmpeg 6.1 with libwebp): image 60 MB → 169 MB.
  thumbs tests run in the image read-only with tmpfs /tmp and
  no-new-privileges, as user kallisti: all pass (WebP, alpha kept).
- Known gaps: an APNG (and an animated WebP, where ffmpeg decodes it)
  gets a still preview; the "edited" message can in a narrow window
  arrive before the "message" (then no preview until reload); the resend
  makes clients fetch the link preview again; preview files do not count
  toward the quota; a crash can leave `.preview-*` / `kallisti-preview-*`
  temp files; refused small pictures are scanned at each start (one
  query); non-square pixels (SAR) are not corrected in video thumbnails;
  old octet-stream uploads with media names get none. The systemd unit's
  MemoryDenyWriteExecute applies to ffmpeg too (not checked).

## Save feedback (2026-10-20)

- One pattern for controls that save on the server: `lib/saveFeedback`
  (pure: idle → pending → saved | error, and which event confirms which
  request), `createSave` (runes), `SaveStatus` ("Saved" for 2 s, or the
  error, next to the control). Buttons show "Saving…" and are disabled.
- `connection.request` / `ask` / `sendAwait` return a promise: settled by
  the confirming event, a manage error (now at the waiting control, not
  the toast) or a Mumble PermissionDenied, or "No answer from the server"
  after 5 s. A checkbox or select goes back to the server's value then.
- The server sends no request ids: answers are matched in order. A refusal
  goes to the oldest waiting control; an event confirms the oldest request
  it matches. Mumble requests (bans, registered users) are done when the
  state holds the change (asked for again after the change).
- Dialogs (channel, members, moderators) now close when the server
  confirms, not at once; Mumble's own channel edits still have no answer.
- Not changed: user menu role changes, kick/ban/time-out dialogs, channel
  recording (they close at once; the result shows in the lists; errors
  stay toasts); the status dialog (errors come as chat errors); the bio
  (saved on blur, no status). Known gaps: a refusal of a request sent
  without waiting (list loads) can go to a waiting control; a broadcast
  from another admin can confirm early; an answer after the dialog closed
  is lost; a plain (non-manage) error makes the control wait 5 s.

## Background disconnects (2026-10-20)

### Android and web (part 3)

- On Android, in voice, the voice mix plays through an `<audio>` element
  (MediaStreamAudioDestinationNode → srcObject) with a Media Session
  ("Voice: <channel>", the server name, the app icons). Pages that play
  media are not frozen in the background. lib/voice/output.ts: one output
  bus per AudioContext (it outlives reconnects); lib/mediaSession.ts:
  the pure session data.
- Only on Android (user agent): desktops do not freeze pages, the element
  path adds a little latency there, and with a Media Session the media
  keys (play/pause meant for another player) would leave voice. iOS has
  known trouble with stream-fed `<audio>` while the microphone is open,
  and suspends PWAs anyway.
- No double output, no silence: the bus goes to the speakers until the
  element fires `playing`, and back on `pause`. A refused play() keeps the
  direct path; it is tried again on the next touch. Output device:
  setSinkId on the context and the element. Per-user volume, deafen, the
  music buffer and the limiter sit before the bus; the echo test, stream
  audio and the sounds keep their own direct paths.
- Actions: pause, stop and hangup leave voice (Kallisti servers); plain
  Mumble servers have no place outside voice, so they deafen there.
  playbackState stays "playing" (a "paused" notification could be swiped
  away, and that pauses the element).
- Rejoin: Client already restored mute/deafen first and then the channel.
  Gap fixed: until the server confirms the rejoin (or 5 s pass), the old
  channel counts as ours. Before, the new client reported "not in voice"
  at once, so a second drop during the rejoin forgot the channel. The
  media session stays across the client swap: the old client's teardown
  releases it after 1 s, and the new one takes it over before that.
- No wake lock: the goal is calls with the screen off.
- Checked headless (Chromium, Android user agent stubbed, a warmload
  speaker in General): joining plays the element (live audio track),
  sources keep starting, the bus is off the speakers, metadata and
  "playing" set; leaving pauses it and clears the session. A server
  restart: rejoined General muted, the element never paused. Desktop user
  agent: no element, the direct path as before.
- Not tested: output.ts has no unit tests (DOM and Web Audio glue; the
  decisions are in the tested pure parts). Not checked on a phone: the
  media notification and keep-alive with the screen off, echo
  cancellation on speakerphone, and the latency of the element path.

## Background disconnects (2026-10-20)

- Cause: the bridge pinged the browser every 10 s and closed it after 30 s
  without a pong. A suspended page (macOS app in the background, Android)
  cannot answer. Now bridge.Options.PingGrace (default 2 min): each ping
  waits up to the next one, the last pong time decides. Mumble pings go
  upstream every 10 s as before.
- A page that does not read: the bridge's writes to it block (no
  timeout), so it stops reading upstream. mumbled now gives web clients a
  3 min write timeout (was 10 s, the bridge closes the pipe when it gives
  up); voice is dropped, other frames queue (512, a full queue still
  disconnects). Sidecar text overflows its outbox and resyncs. A ping
  frame write can still time out (5 s, coder/websocket) if the socket is
  full at exactly that moment; data writes normally hold the lock then.
  Murmur upstream: not checked how long Murmur waits for us.
- macOS app: window config backgroundThrottling "disabled" (Tauri sets
  WKPreferences.inactiveSchedulingPolicy to none, macOS 14+) and
  background::no_app_nap (NSProcessInfo activity, user initiated, idle
  system sleep allowed) for the app's lifetime. Compiled and unit-tested
  on macOS and Linux (podman); not run on the dev Mac.

## Desktop links and video (2026-10-20)

- Links: wry answers no new-window request unless the app sets a
  handler. So target="_blank" did nothing in WKWebView (macOS) and
  WebKitGTK (Linux); WebView2 (Windows) opened its own bare browser
  window. Links without a target (the file card) navigated the app
  window itself (an MP4 that WebKitGTK could not play: nothing visible).
- Fix: lib/externalLinks.ts. In the app, a click or middle click on an
  http(s) link to another origin opens the system browser
  (tauri-plugin-opener, capability: open-url for http and https only).
  The listener is on the document and runs after the app's handlers, so
  plain clicks on channel links, message links to this server and images
  (lightbox) stay in the app (they call preventDefault); modifier and
  middle clicks open the browser, like a new tab. The players' download
  buttons now open the file in the browser too (cross-origin: the
  download attribute does not count; media is served inline). The plugin's own click script is
  off (it also takes mailto: and tel:). The recording download link goes
  into the document for its click, so it is caught too.
- Linux video: WebKitGTK plays media with GStreamer. The AppImage had
  plugins-base and -good only: H.264 and AAC gave MEDIA_ERR_SRC_NOT_SUPPORTED
  (canPlayType ""), the client showed the file card. VP9/Opus WebM
  played already. Tauri's linuxdeploy GStreamer plugin copies the whole
  system plugin directory, so CI now also installs gstreamer1.0-libav.
  plugins-bad is not necessary (no h264parse needed for progressive MP4).
- Checked in podman (Ubuntu 22.04 arm64, WebKitGTK 2.50.4, Xvfb), the
  real AppImage with a probe page as its frontend (same binary, other
  frontendDist): before, H.264/AAC MP4 and M4A error 4; after, canplay,
  currentTime 2.9 s after 3 s, ended at 5 s (a 5 s H.264+AAC clip and the
  mediasize fixtures). xdotool clicks on a target="_blank" link, a plain
  link to another origin and a middle click reached a fake xdg-open.
- Size (arm64 AppImage): 108.1 MB before, 150.3 MB after (+42 MB:
  libavcodec, libavfilter and their codec libraries). Ubuntu's FFmpeg is
  built with GPL parts (x264, x265), so the AppImage now carries GPL
  libraries; H.264/AAC decoding can need patent licenses in some
  countries, as for the Linux distributions that ship it.
- Not done: the .deb and .rpm use the system GStreamer; they do not
  depend on gst-libav. macOS and Windows not run (same wry code paths).

## Multiple sessions (2026-10-20)

- mumbled: a registered account (also a guest account) may connect up to
  10 times (`maxSessionsPerUser`). At most one session is in a channel,
  the voice session, the one others see. The others are text-only: in
  `TextOnly` (`NoChannel - 1`), which is no channel. No client has it in
  `seen`, so the existing broadcasts leave them out without new checks;
  the session itself always gets its own UserState (`client.sees`). They
  get the channel tree and the users, no voice in or out (also not by
  whisper to their session, no listening), and others cannot move them.
  Unregistered guests keep one session (findGhost, as before).
- A new web session joins text-only when the account has a voice
  session. A text-only session that joins a channel takes voice: the
  voice session moves to `TextOnly` (its own client sees itself there,
  others see it leave and the other join) and the server mute, deafen
  and priority go along, so a second tab cannot shake off a mute. When
  the voice session disconnects, the newest text-only session goes to
  root, but only where root is a lobby (elsewhere root has voice, and the
  tab would open its microphone).
- Texts written in a text-only tab reach Mumble clients from the voice
  session (they do not know the text-only one, and showed "Server");
  without a voice session the text starts with the name. When the voice
  session leaves, the others keep its server mute.
- A Mumble client cannot be text-only: it takes voice when it connects
  (and the channel of the web session), a second Mumble client replaces
  the first as before, and a web session that joins voice kicks it ("You
  joined voice on another device"). So at most one non-web session.
- Hooks see `TextOnly` as `NoChannel`: a text-only session joins and
  leaves from NoChannel to NoChannel (the sidecar updates the online set
  then), taking voice is a join and losing it a leave (streams end, as
  when leaving). `UserChannel` has none for it, so it can neither stream
  nor watch; it may still read a voice channel's chat it may enter.
- `Online()` has one session per user: the voice session, else the
  oldest. It counts text-only sessions, so pushes stay off while any
  session is connected.
- Moderation: a kick or ban removes every session of the account; a ban
  also covers the certificates and addresses of the other sessions (a
  Mumble client logging in with the account's password may have its own
  certificate). One entry per session at most, without what the ban
  already has (full addresses, as single bans). A time-out
  mutes every session. Channel time-outs, message rules and slow mode
  were per user id already.
- MaxUsers counts users in channels only; more sessions of an account
  that is in already are not refused.
- Sidecar statuses: still by session (clients look them up by the
  session they see), but each session of an account shows the account's
  status, idle only while all its sessions are away (as Discord). A
  status is sent again for every session whose shown status changed
  (`Conn.sent`). Typing no longer goes to the typist's other tabs.
- Web: `isTextOnly`, `voiceElsewhere` and `listedUsers` in the protocol
  package. A text-only tab shows root, is not in voice (the microphone
  stays closed), lists the account once, and the voice bar says "In
  voice on another device" with "Join voice here" (the channel the other
  device is in). Losing voice to another tab leaves the voice chat for a
  text channel. Device names are not shown: the bridge does not tell
  them, and the Mumble state alone (another session with our user id in
  a channel) is enough for the bar.
- Notifications: of several tabs of one account in one browser, only the
  tab used last (focus or shown) shows desktop notifications, with its
  own rules (lib/notifyLeader.ts, a BroadcastChannel per server and
  account). A tab that ends without "bye" (a crash) keeps the others
  quiet until one is used again. Other devices notify as before.
- Manual check (headless Chromium, fake microphone): two sessions of
  alice and one of bob. Both alice tabs connected, bob listed alice once.
  Voice on the first tab: the second showed "In voice on another
  device"; "Join voice here" moved voice, the first tab showed the bar,
  bob still saw one alice and heard the active tab (speaking ring), the
  text-only tab heard nothing. Closing the voice tab brought the other
  into the lobby, still online for bob.
- Read marks sync live: a stored mark (a web tab's mark-read, or a
  Mumble client reading a chat or a direct message) goes as
  `{"type":"read","channel","message"}` to the account's web sessions
  (`Hub.setReadMark`); the client raises `reads`, so unread marks,
  mention counts and the title follow. The sender gets it too: a tab
  re-sends marks above its `reads` on every event (for reconnects), so
  without the echo two tabs ahead in different chats sent theirs to each
  other forever.
- Gap: the connections view lists every session but does not mark
  text-only ones.

## Review 3: server races (2026-10-20)

- A password change saves with `store.ChangePassword`: one transaction
  that updates only while the session is live and the hash is still the
  one checked. A request stalled before its body loses to the owner's
  change (401), and it does not end the owner's session.
- `may` and `userMay` fail closed for a text channel that is gone
  (`textMirror`): mirror 0 means public only for a channel that exists.
  A chat found before its channel was deleted reaches nobody.
- `manage` authorizes with `manageMu` held: a channel moved out of a
  category meanwhile is checked in its new category.
- `NewMessage.Thread`: `AddMessage` checks the thread row in its
  transaction. A thread deleted since the post found it refuses the
  post, and its upload stays unused (for the cleanup).
- Gap: a post to a text channel that staff delete at the same time is
  still stored (hidden channel, comes back if staff restore it).

## Review 3: voice sessions and bans (2026-10-20)

- Voice handoff keeps the old voice session's channel (`State.held`)
  until the new session is in: a Mumble client or tab that took voice in
  the account's temporary channel joined a deleted channel (later a nil
  dereference on a permission refresh). `takeVoiceLocked` returns the
  release; `enterLocked` is the one way into a channel for a session,
  also when it makes a temporary channel (single voice session, server
  mute goes along).
- Slow mode: `MumbleClient` returns who a web session is too (ok: false),
  so binary web texts take the account's turn, not the session's.
- A moderator's move from one moderated channel into another counts the
  moved channels against the destination's 50.
- Bans: a Mumble client's own certificate is banned along with its
  account's; the list is published under the server lock, and `join`
  checks bans again, so a connection admitted meanwhile does not stay.

## Review 3: client state (2026-10-20)

- Push: the opt-out count is in localStorage (`pushOffs`), so an
  opt-out in another tab stops a sync that is running here.
- Reconnect: `Place.session` is the session that held our voice. After a
  reconnect we take voice back only from that old session (the server
  has not let it go yet), not from another device that joined meanwhile.
- Read marks: only a visible tab marks new messages read (`autoSeen`); a
  tab that becomes visible catches up.
- Streams: a watch stopped before the stream socket opens asks for
  nothing (the viewer entry is checked after the await).
- Resync: the client answers `resync` with a `resync` request; the
  sidecar sends a fresh ready (`readyFor`, `resendReady`), with nothing
  told to others. So follows, threads, read marks and the rest come back,
  not only history. A ready keeps known archived threads, as a channel
  list does.
- Save feedback: a channel change is confirmed only by a list with that
  change (names compared as the server writes them, without case and
  marks: Go and JS lower case differently); a privacy change by the
  private list. Before, the privacy change's own "channels" event
  confirmed a rename in the same save.
- Drafts live in `Client` (`Drafts`), so they survive panes that close
  (a thread over the channel on phones, a closed thread pane). Checked
  headless on desktop and phone sizes.

## Review 3: media, streams and CI (2026-10-20)

- WHIP: offers still coming count against MaxSessions on their own and
  must arrive within 10 s (a read deadline through
  http.ResponseController); a new stream key ends the reads of that
  account. An error answer sets "Connection: close": else net/http reads
  the rest of a Content-Length body before it answers, without any
  deadline (the tests with io.Pipe send chunked bodies and did not show
  it).
- Stream thumbnails: Speak is checked again on each accepted thumbnail
  (as for frames). The server decodes the JPEG header: JPEG only, at most
  1024 pixels a side. Clients make them 320 wide and now at most 1024
  high (a narrow, tall window).
- Previews: ffmpeg gets `-max_pixels` (imagesize.MaxPixels), so the coded
  frames are checked, not only the container's display size. A full disk
  (ffmpeg's stderr) or an ffmpeg that does not start is no longer
  ErrFailed, so no NoPreview mark; the next start tries again.
- Recording: the silence before a packet must fit in the bytes left
  (512 per second, as for the final padding), else the recording stops
  at the size limit without writing it.
- RPM: depends on gstreamer1-plugin-libav (Fedora 43 and 44, standard
  repos). Fedora builds it on ffmpeg-free: AAC decodes, H.264 only through
  OpenH264 (Cisco repository); full codecs need RPM Fusion's
  libavcodec-freeworld. Not tested on Fedora with the app.
- CI: test.yml runs `cargo test --locked` for the desktop app on Ubuntu
  22.04 (an empty apps/web/dist is enough for generate_context). It
  passed in a Linux container; the Murmur tests skip without
  KALLISTI_TEST_MURMUR.

## Review 4 fixes (2026-10-20)

- Drafts live in App.svelte and go to Client as a prop: a reconnect makes
  a new Client (`{#key connection}`), and the drafts stay. A new login
  starts without them.
- A password change or "log out everywhere" also kicks the Mumble clients
  that logged in with the account's password: they have the account's
  certificate hash (see identifyAccount), web sessions are left to the
  bridge. Certificate-registered Mumble users are not accounts and stay.
  A login that checks the old password just before the change can still
  join just after the kick (not fixed: rare).
- Combined channel saves compare names with toUpperCase only: the server
  keeps accents ("cafe" and "café" are two names). A rename to a Turkish
  dotted İ now gets no confirmation (JS lowers it to i + U+0307).
- Native history catch-up for a text mirror checks Sees at delivery: move
  hooks arrive late.
- Small request bodies get a 10 s read deadline (config.LimitBody): auth
  POSTs, upload posts until the token is checked, and WHIP offers from the
  start of the request. Production's Caddy passes slow bodies on without a
  timeout. The deadline also ends net/http's wait for the unread rest of
  a body after an early refusal.

## Stale media playback (2026-10-20)

- Attachment players stop and drop their source when they unmount
  (`releaseMedia`, as `{@attach releaseOnDestroy}`). Before, the last one
  that played stayed the media-session target after a reconnect, and a
  media key or headset resumed it hours later.
- Stream sound is dropped while the AudioContext is not "running"
  (suspended, WebKit "interrupted"): its clock stands still, so chunks
  piled up (about 690 MB per hour) and played as one burst on resume. The
  playout re-anchors on the first item after the clock runs again.
- Checked in headless Chromium: a draft stays after the server restarts
  and the client re-mounts.

## Hide a shown spoiler again

- A second click (or Enter/Space) on a shown text spoiler hides it again (`toggleSpoiler` in lib/markdown). A link in a shown spoiler opens instead, and a click that ends a text selection does nothing. Discord keeps spoilers shown; users asked for a way back.
- Shown image and video spoilers get a small "Hide" button at the top right. The image itself still opens the viewer.

## Self-mute survives a refresh

- Self-mute and deafen are saved per server in localStorage ("muted", "deaf" or "") and sent right after connecting, before any voice join, as Mumble keeps them across restarts. A reconnect still uses the in-memory place. Saving starts only after the restore, since a new session starts unmuted. Checked headless: mute, deafen and unmute each survive a reload.

## Personal emoji

- A message or reaction refers to a personal emoji as `<:name:id>`, where id is the image (as Discord does). Markdown runs on escaped text, so the client keeps `&lt;:name:id&gt;` away from emphasis and adds the image after sanitizing, as for server emoji. Mumble clients get `:name:` (mumbleText, also in reply quotes and history). The user types `:name:`. The client sends the reference (toRefs) and changes it back for an edit. Server emoji keep their names: a personal emoji cannot get a server emoji's name.
- Images are stored once per sha256 (`emoji_images`). A user's list is `user_emoji` (name, image id). Images stay when nobody has them, because messages show them. Limits: 50 emoji per user, and 50 new images per user in 24 hours (a user who deletes and uploads again cannot fill the disk). New images follow the message rules for uploads from new accounts. Adding an image from a message is not a new upload.
- A reaction is stored under the image's first name. Then everyone's reactions with the same image are one chip, whatever names they gave it.
- Moderator removal keeps the image row without data. The same row blocks the hash and keeps the name for reactions. Clients get the removed ids (ready, `removed-emoji`) and show `:name:` text. Personal images are `Cache-Control: private`, so shared caches drop them, and a browser's cached copy is not shown. The block only stops the same bytes: a changed image gets through.
- Using an emoji does not require it in your list on the server side: anyone can post `<:name:id>`. The images are public anyway (like server emoji), and moderators can remove one.
- Checked headless (agent-browser, media devices stubbed): add an emoji in the settings, use it with a `:bl` suggestion, a second user sees it and adds it from the message under another name, reacts with it from the picker (one chip), SuperUser removes it for everyone (text in messages and reactions, 404 for the image), and an upload of the same image is refused. Guests have no "My emoji".

## Fediverse login

- "Log in with the fediverse" (Mastodon API: Mastodon, Pleroma/Akkoma, GoToSocial, ...): the server registers an app per instance (cached in `fedi_apps`), sends the browser to the instance's OAuth page (authorization code, PKCE S256) and reads `verify_credentials` at `/api/fedi/callback`. A remote account is keyed by host + remote id; the handle (`@user@host`, host in ASCII) is only for display and follows renames.
- Scope: apps are registered with `read:accounts profile`; servers that refuse `profile` (Mastodon before 4.3) get `read:accounts`. An `invalid_scope` from the authorize page retries once with `read:accounts`. The token is not kept, and not revoked: Mastodon deletes apps without tokens after a day. An app without a login yet is registered again after an hour for the same reason.
- State: a random state in an HttpOnly, SameSite=Lax cookie (path /api/fedi), compared with the callback's state, used once, 10 minutes. Flows and sign-ups live in memory (bounded); a restart ends them. The callback always redirects to the public origin (first ALLOWED_ORIGINS entry); no `*` origin, no fediverse login. Results come back as `?fedi=register|linked|error&reason=<code>`; the client shows only its own texts for known codes.
- Register: an unlinked remote account gets a "choose your name" step (the remote user name, cleaned to valid names). Same rules as other new accounts: name checks and the browser check (captcha) when admins turned it on. A fediverse login is little proof (anyone can run an instance), and the check costs a second or two once. Message rules for new accounts apply by account age, as before.
- Link: needs the current password if the account has one (a stolen session must not add a lasting way in), and the same session at the callback. A guest account that links becomes a normal one. One remote account per account. Unlink is refused while it is the only way to log in; a password-less account sets its first password without an old one (also for Mumble apps). Password logins to password-less accounts are refused (with the dummy hash, same timing).
- First password of a fediverse-only account: needs a fresh login with the linked fediverse account ("Confirm with <host>", `purpose: "reauth"`), in the same session; the callback checks host + remote id and keeps a proof for 5 minutes, used once. Without it `/api/password` answers 403 `code: "reauth"`. A stolen session can no longer set a password and lock the owner out. With fediverse logins turned off, no check (no other way back in). Checked headless (with a `navigator.mediaDevices` stub for the settings dialog).
- SSRF: one guarded client (`fedi.NewClient`, `netguard` shared with link previews): the dialer checks the address really dialed (DNS and redirects included), public addresses on port 443 only, HTTPS only, 3 redirects, 10 s, 64 KB bodies, no proxy. `FEDI_INSECURE_DEV_INSTANCES=1` (dev only, off by default) allows local and plain-HTTP instances, for `server/cmd/fakefedi`. `FEDI_LOGIN=0` turns the feature off.
- Verified handle: opt-in per account; `GET /api/fedi/handle?name=` gives it only when on, and the user card shows it with a check mark.
- Avatar: a fediverse sign-up takes over the remote avatar (`avatar_static`, else `avatar`; Mastodon's `missing.png` and Pleroma's `avi.png` defaults are skipped); a link only if the account has none. Downloaded after the answer (5 s, 2 MiB) through the guarded client, header checked (8192 px a side, 16 Mi pixels) before decoding in pure Go (`avatarimg`: JPEG, PNG, GIF first frame, WebP), then the middle square at 128 px, JPEG q85 on #2b2d31, as the web client's `avatarEncode`. `mumbled.SetAccountAvatar` registers an account that never visited (as its first visit would) and updates online sessions at once. Failures only log. EXIF orientation is ignored (instances strip it from avatars).
- Checked headless against the fake instance: register (with a name clash), log in again, link a password account (wrong password refused), log in through the link, unlink refused for a password-less account, handle hidden until opted in, error notice on the login screen. The settings dialog closes the headless Chromium tab (also on main, before this work), so its fediverse section was checked through the API only.

## Long press on a reaction

- On touch screens a long press on a custom-emoji reaction opens its menu ("Add to my emoji", "Remove for everyone…"), as right-click does with a mouse; iOS has no right-click. Elements marked `data-own-press` keep their own long press, so the message sheet does not open as well. Reactions with plain emoji keep opening the message sheet.

## Pending attachment survives a reconnect and a reload

- On the Android PWA the file picker sends the app to the background: the connection drops (the Client is new after the reconnect) or Android kills the page. A picked file now lives per chat in `lib/pendingFiles` (created in App per login, like drafts) and also in IndexedDB, keyed by server origin + Mumble server + user name + chat key. It comes back when its chat opens, also after another chat or a reload; it goes on send or discard. A failed upload keeps it.
- Bounds: only files within the upload limit (50 MB when unknown), the newest 5, 24 h. IndexedDB failures (private mode, quota) only leave the file in memory. Store calls go through one queue, so a delete never overtakes its put.
- The IndexedDB adapter has no unit tests (no fake-indexeddb); the logic is tested with an in-memory store. Checked headless: pick, server restart (reconnect), reload, send.

## Phone: member drawer no longer pushes the chat aside

- Opening the member drawer focused its first button while the drawer was still off-screen to the right; the browser scrolled the `overflow: hidden` layout to show it (280 px), and the chat slid back. The channel drawer sits to the left, where a container cannot scroll, so it never showed this. Fix: `focus({ preventScroll: true })` and `overflow: clip` on the phone layout. Checked headless at 390 px: the layout's scrollLeft stays 0.

## Sending an attachment across a dropped connection; send nonces

- An attachment send used to count as done once the upload worked: the chat message went out on the WebSocket without a check, so a drop right then cleared the file without a message. Now the send waits for the server's answer (`connection.sendConfirmed`, 15 s): our message confirms it; an error, a timeout or a closed connection keeps the file and caption, and the pending row shows "Not sent: <why>".
- Nonces, as in Discord: a `send` can carry a `nonce` (at most 64 bytes). The server keeps it with the message (columns `nonce`, `nonce_author` with a partial unique index; the author is the user id for registered users, else the name) for 24 h. A send with a known nonce stores nothing: the sender gets the stored message again, nobody else does. This check runs before slow mode, so a retry is never refused for its own first try. Only the sending connection sees the nonce on the broadcast. Without a nonce (Mumble clients, old web clients) nothing changes. If the insert fails because another connection stored the same nonce first, that message is the answer.
- The pending store keeps one nonce per picked file (also in IndexedDB), so each retry uses the same one. The client confirms by nonce; with servers that do not echo nonces, it falls back to the upload key in the file URL.
- No double send after a reconnect: `pendingFiles.send` marks the file as sending in the shared store (above the `{#key connection}` re-mount). The new pane shows "Sending…" and does not send it again. When the old send ends, the file goes (success) or stays with its error. Seen headless: the old Client finishes on the new connection, because Svelte props still read the parent's current `connection` after the re-mount.
- Svelte wiring (ChatPane watch and status) has no unit tests. Checked headless with a dropped echo (retry, one message stored), a delayed upload and a closed socket ("Sending…" in the new pane, a second Enter sends nothing, one message).
- Refusals carry the nonce: while the server handles a `send` with a nonce, every error frame it answers with gets that nonce (set in `Handle`, added in `Conn.send`). This covers all refusal paths at once: rules, rate limit, slow mode, guests, time-outs, channel access, upload, store. The client fails a waiting send only on an error with its nonce. Errors of other requests no longer show "Not sent" for a stored message. With servers that send no nonces, a refused send ends on the 15 s timeout.
- Open: each retry uploads the file again; the unused upload is cleaned up later.

## A refresh keeps the open channel and voice

- The reconnect place (view, thread, voice, session, self-mute) is now also saved per tab in sessionStorage (`lib/place`: `savePlace`, `loadPlace`, key per site, server and user). `start` loads it, so a refresh comes back through the same `resume` path as a reconnect: `rejoinTarget`, mute before join, fallback for removed channels. Logout (`leave`) clears it.
- Self-mute after a refresh comes from the tab's place, not the per-server localStorage value. That value is only for a new tab. The reason: another tab may have saved its own state since. Until the server echoes the restored mute, the reported place keeps it, so a second refresh right away does not come back unmuted.
- The saved session is used only if the page saved it on `pagehide` (`left`). A fast double refresh needs it: the old session can still be in voice when the new one syncs, and `rejoinTarget` accepts only our old session there. A duplicated tab copies sessionStorage while the first tab is still open, so its copy has no `left` and does not take voice.
- A message or invite link in the URL wins over the saved view and thread; voice still comes back. Direct chats find the partner's session again by user id (guests: by name).
- The audio context starts suspended after a refresh (no user gesture). The first pointer or key input resumes it; in voice, "Click anywhere to turn on sound" shows until then (`soundHint`). Not in the Linux app, where `startAfter` holds the context on purpose.
- Svelte wiring has no unit tests (App, Client); the save/load and hint logic do. Checked headless: thread restore after a refresh; in voice, muted and deafened, back in voice after a refresh and after a fast double refresh; hint shown, gone after a click; a message link in the URL beats the saved voice view. Headless Chromium had no fake microphone ("No microphone"), so voice was checked for channel and state only, not for audio.

## Fediverse login on Pleroma and Akkoma

- Pleroma's AppCleanupWorker deletes apps without a token after 15 minutes (every 10 minutes). We kept unused apps for an hour, so a later login sent a client_id the instance no longer knew: its authorize page showed no app and Approve said "Invalid Username/Password". Unused apps are now registered again after 5 minutes, which leaves a user 10 minutes on the instance's page.
- Pleroma and Akkoma accept the "profile" scope as a string, but its token cannot read the account. The app asks for "profile" only when /api/v1/instance reports a plain Mastodon version of 4.3 or later; everything else gets read:accounts.

## Restart audio on entering voice and on output device changes

- A long-open tab kept the AudioContext made at login. After the Mac's output device changed, Chrome resampled that context, and stereo music in voice crackled until a refresh. Now `restartAudio` (App) makes a fresh context, moves everything to it and then closes the old one (`swapContext` in `lib/voice/restart`).
- Triggers: a join from outside voice (`restartOnJoin`, in the click, so the new context may start), and in voice a `devicechange` that changed the audiooutput list (`watchOutputDevices`: debounced, compares id, group and label, so microphone-only changes and the same list do nothing). There is no restart on moves between voice channels or on reconnects.
- What moves: playback and capture (`createVoiceAudio`: new objects, so Client's effects set the volumes, deafen, buffer, listen-only, output device and microphone again), stream sound (`streams.setAudio`), and the Android `<audio>` element and media session (`closeVoiceOutput`: the old one stops, and its late release does not end the new session). The Linux `startAfter` hold is applied to every new context.
- A restart without a gesture (devicechange) can give a suspended context. Then the "Click anywhere to turn on sound" hint shows, as after a refresh.
- Device-change restarts run only while the page shows (`documentPage`). A phone in a pocket keeps its voice when a headset connects. A change while the page is hidden is checked when it shows again, against the list of the current context, so a change that was undone does nothing. "Shows again" is not a gesture: iOS Safari can still give a suspended context there (then the hint shows). This is not checked on a real phone.
- Device-change restarts keep the open microphone. The old capture's `close(true)` hands its stream over (`MicHandover`), and the new capture's `start` uses it if the constraints (device and filters) are the same and the track still runs. Otherwise, it closes it and asks again. There is no new permission prompt in Firefox and no gap in the microphone. On the default device, the microphone stays on the input that was the default before. A join restart opens the microphone as before. A held push-to-talk key still must be pressed again.
- Headless Chromium on the Mac crashes once a page listens for real `devicechange` events. This also happens on main when the settings dialog opens. For the check, the listener, enumerateDevices and getUserMedia (an oscillator) were stubbed. Checked: a join from the lobby closes context 0, and playback and capture (worklet, media source) run on context 1. A microphone-only change does nothing. An output change moves everything to context 2, and a second user's voice plays on in the new context. After leaving voice, there is no listener and no restart. Svelte wiring has no unit tests; the decisions and the swap logic do.

## AVIF inline, JPEG XL by its preview

- Go's `http.DetectContentType` knows neither format. `mediasize.DetectType` adds AVIF (an `ftyp` box with the brand `avif` or `avis`, major or compatible) and JPEG XL (`FF 0A`, or the container signature). Uploads and link preview images use it. Emoji do not: imagesize cannot check AVIF.
- AVIF size: the largest `ispe` in meta/iprp/ipco (a grid's tiles have their own, smaller ones), swapped by an odd `irot`. Sequences without `ispe` fall back to the MP4 track. The same inline limits as other pictures apply, and one oversized `ispe` refuses the file (download). The `ispe` is only what the file claims: the browser decodes the AV1 frame. The server never decodes AVIF, so only the client is exposed, and browsers have their own limits.
- No ffmpeg previews for AVIF. The image's ffmpeg (Alpine 3.21, 6.1.2) fails on grids, and with `-noautorotate` it drops `irot`. ffmpeg 9 on the Mac composes grids. AVIF files are small, so the original is fine in the chat.
- JPEG XL is not inline: only Safari shows it. It is stored as `image/jxl`, served as an `application/octet-stream` attachment, and always gets a preview (`files.previewedTypes`), even a larger one. The size comes from the codestream's SizeHeader (about 40 lines of bit reading), turned by the orientation. Checked in alpine:3.21: `jpegxl_pipe` reads codestream and container, and libjxl turns by the orientation itself.
- ffmpeg 6.1 with libjxl loops at full CPU when `-max_pixels` refuses a frame. ffmpeg checks the width rounded up to its stride (up to 64), so a header just inside our limit can trip it. JXL with `(w+63)&^63 * h` over the limit gets no preview job. The 20 s SIGKILL in thumbs is the backstop.
- Client: `attachmentView` decides picture, player or file card. A picture whose original also fails to load (AVIF in an old WebView) becomes a file card. Before, the `img.src !== u.url` check compared an absolute with a relative URL and could reload without end. JPEG XL shows only by its preview; the lightbox shows the preview with "Download original".
- Fixtures: AVIF from ffmpeg (libsvtav1: still, animated) and avifenc in a Debian container (2x2 grid, `--irot 1`); JPEG XL from cjxl (codestream, container, jxlp boxes with an Exif orientation).

## iOS safe area and a simpler phone header

- iOS home screen app: `viewport-fit=cover` and the `black-translucent` status bar. `--safe-top/right/bottom/left` on `:root` default to `env(safe-area-inset-*)` (0 on desktop and Android); headless checks set them in an injected style. Each column (rail, sidebar, chat, side panels) gets a transparent `border-top`/`border-bottom` of the inset: its own background fills the strip under the status bar and the home indicator, and unlike padding the border stays out of the scrolling area. The rail column is wider by the left inset, the layout pads the right one. Modal dialogs get the insets through `:where(dialog:modal)` (no specificity: Lightbox sets `inset: 0` and pads itself). Toasts, banners, sheets and the phone emoji picker pad too.
- The light theme gets the `default` status bar (dark text) from theme.js; `black-translucent` always has white text. Not checked on an iPhone, nor whether iOS reads the meta after a script changed it.
- Checked headless: 1280, 800 and 390 px pixel-identical before and after (also the phone drawer); with emulated insets (47/34 px, landscape 47/47/21 px) header, drawers, composer, user panel and sheets clear them. Known gaps: context menus and the desktop emoji picker fit the window, not the safe area; the 1 px divider of the search and thread panels fades in the inset strips.
- Phones (max-width 700px): the chat header shows the menu, the name, a search icon and the member list (`headerParts` in lib/chatHeader). The name opens `ChannelSheet` (on `BottomSheet`, now shared with MessageSheet): topic, pins, threads, copy link (text channels, `/channels/<id>`, which now opens the chat), notification settings (as in the channel menu; direct chats only mute). The search icon puts a full-width field over the header with a back button; Escape and back close it and focus goes back to the icon. Results show in the panel as before. Voice chats: topic and pins only. The thread pane keeps its header.
- BottomSheet gives focus back on dismiss (Escape, tap outside, drag), not to text fields (no keyboard popping up), and keeps Tab in the sheet.
- Svelte markup and CSS have no unit tests (no TDD there); the header decision, channel links and mute times do. Checked headless at 390 px: header items, sheet (mute and unmute, level, pins, copy link, focus trap and restore), search open/close and results, a DM's sheet; 1280 and 800 px unchanged. Voice views were not checked headless: joining voice closes the headless tab without a media stub.

## iOS 26 edge blur and pinch zoom in the installed app

- The smear at the top of the installed iOS app is iOS 26's Liquid Glass scroll-edge ramp: system chrome drawn over the web view, about 40pt below the status bar, whatever the status bar style. No CSS or meta tag turns it off, so in the standalone app in portrait `--safe-top` grows by 2.5rem and the strip (in each column's colour) covers the ramp; the header sits below it. Not verified on a device yet.
- The installed app (display-mode standalone) no longer pinch-zooms the page: `touch-action: pan-x pan-y`, and `gesturestart` is cancelled for iOS. Browser tabs keep zoom.
- Follow-up: an app was marked used as soon as the token exchange worked, before the account was read. An app registered with "profile" before the version check (pwm on darkdork.dev, Pleroma 2.10.2) was so kept for good, and every login got 403 from verify_credentials. Now an app counts as used only once the account was read, and a 401/403 on a "profile" token forgets the app, so the next login registers again with the version check.

## GPU use while the desktop window is minimised (#3)

- The desktop app has backgroundThrottling "disabled" (voice), so the web view kept painting animated GIFs and emoji while minimised. `lib/windowHidden` combines the page's visibility with the Tauri window (isMinimized, isVisible) into one hidden state; `main.ts` sets `html[data-window-hidden]`. Tauri 2 has no minimise event: the window is asked on resize and focus changes, and every 2 s while it has no focus (a focused window counts as shown at once). No Rust change: `core:default` allows these calls.
- While hidden, app.css hides `img` and `video` with `visibility: hidden` (layout and scroll stay, a playing video keeps its sound) and pauses CSS animations. The stream canvas stays: it feeds picture-in-picture. Chat videos only play when the user starts them, so none are paused.
- Wayland does not tell apps that they are minimised, and WebKitGTK keeps the page visible there: on Wayland nothing changes. X11, Windows and macOS work (not tried on real desktops yet).
- CSS has no unit tests (no TDD there); the watcher has (fakes for the document and the window). Checked headless: a hidden page sets the attribute, hides pictures and pauses animations; shown again, all comes back.

## iOS 26 edge blur: a fixed strip, not extra space

- The 2.5rem clearance hid the blur, but the gap above the header was too large. WebKit's source (`LocalFrameView::fixedContainerEdges`) has the rule: it samples 4px inside each edge, and if a fixed box at least 90% of the viewport wide and over 10px tall with one opaque background colour is there, it uses that colour and draws no blur. Apple documents none of this. The installed app now has a fixed `.status-bar` strip of `--safe-top` height in `--bg-main`, and the clearance is gone. Needs a check on an iPhone.

## iOS 27 edge blur and bottom gap: no black-translucent

- Reproduced in the iOS 27.0 simulator (iOS 26.5 shows no blur): with `black-translucent`, the installed app blurs the header whatever is at the top, and the page is only `screen - status bar` tall (812 of 874pt) while it starts under the status bar, so a 62pt gap stays at the bottom.
- Without the meta tag, iOS draws its own status bar and the page fits below it (no gap, `--safe-top` 0). Then the fixed-strip rule works: an 11px fixed strip with `background-clip: text` (invisible) gives the status bar the chat colour and no blur. Checked on iOS 27.0 and 26.5.
- Existing installs keep the status bar style they were added with: remove the app from the home screen and add it again.

## Keyboard gap on iOS

- With the keyboard open, the composer kept the home indicator space (34pt) below it, above the keyboard. `lib/keyboard.ts` says the keyboard is open when the visual viewport is more than 150px shorter than the layout (zoom taken out); then `html[data-keyboard]` sets `--safe-bottom` to 0. Checked in the iOS 27 simulator. The empty typing row (24px) stays below the composer.
- Then the empty typing row also collapses to 4px while the keyboard is open (it moves the composer up when someone starts typing; on a phone the space counts more). A ResizeObserver on the message list keeps a reader at the bottom when the list gets shorter or taller (keyboard, window). Checked in the iOS 27 simulator; no unit test for the scroll (DOM layout).

## No server password field without a server password

- GET /api/server gives `{"password": bool}`: true with Murmur (unknown) or the built-in server with MUMBLE_PASSWORD. The login page shows the server password field only after a true answer or a failed request (older servers), so Kallisti servers without a password never show it.
- The login screen opens on "Log in" (guests on "Guest" after a guest error such as a name in use). The server password field is on the Guest tab only: the built-in server checks it only for names without an account. The perf and voice scripts click the Guest tab first.

## WHIP streams: viewing

- A click on LIVE did nothing in two cases: the streamer's own stream (a WHIP stream belongs to the account's voice session, and nobody could watch their own stream), and a browser without WebCodecs (Safari in Lockdown Mode: no stream client, so no handler). Now an own stream from OBS may be watched (server: `Hub.Watch` allows it for an ingest; client: `watchAction`, and the sidebar's own badge is a button while `obsLive`), and a browser without WebCodecs says so in a toast.
- WebKit decodes our H.264 (Annex B, no description) fine: checked in the iOS 27 simulator's Safari.
- WHIP streams had no preview: browsers make their own. With ffmpeg (as for upload previews), the server now makes a 320px JPEG of a keyframe (`thumbs.Maker.Still`, stdin to stdout, `-f h264`, pipe only): at the first keyframe, then at most every 15 s, one at a time; a stop cancels one in the making. Checked end to end with the server in podman (the sandbox blocks UDP) and ffmpeg's WHIP muxer on the host: the viewer got the preview, the streamer watched their own stream, and a browser without VideoDecoder got the message.

## WHIP: sound drift and streams over a refresh

- Sound from an OBS/ffmpeg stream dropped out in spurts after a while. The sender stamps sound by sample count (the capture device's clock) and pictures by the system clock; they drift apart. The viewer's playout keeps both on one clock and drops sound beyond its 1.5 s lead; simulated with 0.1 % drift, all sound was gone after about 25 minutes (reopening the stream resets the clock, so it came back for a while). The server now keeps the pictures on the sound's timeline (`whip/sync.go`: the least arrival gap per 2 s window gives each track's drift; pictures shift by at most 5 ms each). Left: with the sound's clock 0.1 % slow the playout still pauses 0.25 s every few minutes and restarts about every 25 minutes (a fast clock plays clean); playing a little slower while the lead is low would smooth that.
- A browser refresh ended the owner's voice session and so the stream from OBS. Now the hub's end orphans the feed: the WHIP connection waits up to 20 s for the account's voice session (the same one after a move, a new one after a refresh) and starts the stream there with the next keyframe. Viewers watch it again by themselves (`lib/stream/rewatch.ts`): a watched stream that ended (left the stream list, not closed by the viewer) is watched again when the same user's stream of the same kind comes back within 30 s. Checked end to end in podman: the stream went on as a new session and the viewer's picture came back.
- Review fixes: the account's session lookup also returns a session in text only (a refreshed tab before voice), so the rejoin now waits for a session that may stream (`Handler.voiceFor`, also for new streams); an ingest the hub ended before `start` stored it no longer stays (`Ingest.Ended`, `feed.live`); the viewer's re-watch state is a pure step (`rewatch.follow`, tested) and skips streams the viewer closed. Checked again in podman: a refresh brings the stream back for a watching viewer, and not for one who closed it.

## Fediverse login: large Pleroma accounts

- A login from shigusegubu.club (Pleroma 2.10) failed with "Could not check the login": the client read at most 64 KB of a JSON answer, and Pleroma and Akkoma put the user's front-end settings (`pleroma.settings_store`) into `verify_credentials`, often hundreds of KB. The limit is now 2 MB (`fedi.maxBody`).

## Stickers

- Large pictures sent as a message of their own. Stored like personal emoji, not like server emoji: a deleted sticker keeps its row without the image (a partial unique index frees its name), so old messages show its name; ids are never used again, so images are cached (privately: a deleted one should leave caches).
- A sticker message has `messages.sticker_id` and also the text "[sticker: name]": Mumble clients, search, quotes, previews, push and old web clients need nothing new. Edits are refused. The nonce check comes before the sticker check (a retry after a delete still confirms the stored message), and slow mode gives back the typed text, not the sticker's.
- Upload as emoji (a WebSocket token, then POST /api/stickers), 512 KB, resized in the browser to 320 px (the emoji resize code with a size parameter); animated files kept. Many files at once for packs. Up to 200 stickers, grouped by an optional pack in the picker.
- The picker has its own button next to the emoji button. In a headless browser a background session's pictures are hidden (html[data-window-hidden]): not a bug.

## iOS app: voice in the background

- Tauri 2 builds the iOS app from the desktop crate (`tauri ios`; `gen/apple` is tracked, `Info.ios.plist` is merged into its Info.plist). Global push-to-talk and window calls are desktop only.
- WKWebView inset its scroll view by the safe areas: a white band below the page and the composer too high. The scroll view's `contentInsetAdjustmentBehavior` is now `never` (`ios.rs`); the page keeps clear of the notch and home indicator itself.
- Voice stopped in the background and with the screen locked. With `UIBackgroundModes` audio, WebKit does not mute the microphone (the track never got `mute`) and the page's JavaScript keeps running, but WebKit sets the audio context to "interrupted" when the app goes to the background (just before the page counts as hidden), and our capture and playback both run in that context. A `resume()` once the page is hidden works and voice goes on (`voice/background.ts`, only while in voice: a running context keeps the app awake). A short gap on each switch remains. Checked on an iPhone XS with iOS 27: background and several minutes locked, both directions. Found with a temporary init script that logged to a file in the app's container, and the device log (idevicesyslog).
- The app has no WebCodecs `AudioDecoder` (Safari on the same iOS has it with Opus): stream sound was silent there. It now uses the voice decoder (WebCodecs with Opus, else libopus), which also spares the Linux app WebKitGTK's leak. Going live with sound still needs `AudioEncoder`.

## 24-hour time, stream view height, picture-in-picture note

- Message times followed the browser's language (12-hour for English). A setting "Time format" (as the language, 24-hour, 12-hour) sets `hourCycle`; 24-hour uses two-digit hours ("09:05").
- The stream view had a fixed height (55 % of the window, 40 % on phones). Its lower edge is now a separator to drag (pointer capture) or move with the arrow keys, Home and End: 20 to 80 % of the window height, saved (`streamStage`, three digits).
- In picture-in-picture the floating video is the tile canvas's `captureStream()`, so the canvas must go on drawing: the tile is covered with a note and a button to bring it back, as other players do.
- Checked end to end with the server in podman, an ffmpeg WHIP test stream and headless Chrome (picture-in-picture works there).


## Android app: voice in the background

- Tauri 2 builds the Android app from the desktop crate, as for iOS (`gen/android` tracked, `tauri.android.conf.json`: id `chat.kallisti.mobile`). Toolchain in the home folder: Temurin 21 (mise), the Android SDK under ~/Library/Android/sdk (platform and build tools 36, NDK 29). The APK ran at once; wry asks for RECORD_AUDIO and MODIFY_AUDIO_SETTINGS when the page opens the microphone, so the manifest declares them.
- Voice stopped a few seconds after the app went to the background: logcat showed `AudioRecord stop` 5 s after the activity paused; the process was not frozen. Android records in the background only with a foreground service of type microphone. `VoiceService.kt` is one, with a notification "In voice: #channel" that opens the app; `VoicePlugin.kt` (a Tauri plugin in the app module) starts and stops it, `android.rs` forwards the app command `voice_service`. The web client turns it on while in voice; a channel change only updates the notification, since Android starts such a service only from the foreground. Checked on the user's phone: background and screen off for minutes, both directions.

## Stream controls on phones

- The tile's controls showed on hover only, were small, and in full screen sat under the status bar (Android 15 draws apps under the system bars; the page keeps clear of them with the safe areas, full screen did not). On touch screens (`hover: none`) a tap now shows the controls for 3 s and the next tap picks the stream (`tapAction`), hidden controls take no taps, buttons are at least 44 px and the volume slider wider; full screen pads by the safe areas. The full-screen buttons hide where the browser has no element full screen (iPhone Safari). Checked on the user's Android phone.

## Android app: the keyboard

- pwm: with the keyboard open the channel header was gone. Edge to edge (Android 15+) the keyboard no longer shrinks the window, so the WebView panned the whole page up. `MainActivity` now pads the content view by the keyboard's height (IME insets), and passes no navigation bar inset while it is open (no gesture bar gap above the keyboard): the page gets shorter instead.
- Then the chat jumped back into history when the keyboard opened: the resize fired a scroll event before the ResizeObserver, and the scroll handler decided the reader had left the bottom. A scroll right after the list's height changed now keeps a reader at the bottom there. Checked on the user's phone (guest login): header visible, newest message above the composer, normal again after closing.

## Fediverse login: Sharkey works only once

- Rain (melonbread.xyz, Sharkey): the first login went through, every later one ended on Sharkey's "An error has occurred". Misskey and Sharkey give a Mastodon app whose client_id is base64 of an auth session URL (`<base>/auth/<id>`); `/oauth/authorize` only decodes it and redirects there, and one login uses the session up. We kept the app per instance. `fedi.SingleUse` spots such a client_id, and `fediApp` then registers a new app for each login. The fake instance has `SingleUseApps` for the test.

## Several files in one message

As in Discord: up to 10 files per message (`store.MaxUploads`), each with the size limit of one file and an optional description (alt text, up to 1024 characters).

- Store: the uploads name their message (`uploads.message_id`, `position`, `description`). A one-time migration (`upload_messages` in `store_meta`) copies the old `messages.upload_id` over and clears it, as its foreign key would keep an upload that an edit takes off.
- Protocol: messages carry `uploads` and still `upload` (the first), as the desktop and mobile apps bundle older web clients for a while. `send` takes `uploads: [{key, description}]` and still `upload: key`; the web client sends one file without a description in the old form, which older servers know. `ready` tells `maxUploads`; without it the client allows one file.
- Spoilers stay a file-name prefix (`SPOILER_`), as in Discord: older clients understand it too.
- `upload-token` with `count` gives the tokens for all files in one request: one request per file hit the WebSocket rate limit (burst 10) with 10 files.
- Composer: the waiting files are a list per chat in `pendingFiles` (old IndexedDB rows with one file are read). The list is `$state.raw`: IndexedDB cannot store Svelte's state proxies, and the write failed without a message. A send uploads the files one after another ("Uploading 2 of 5…"); a failed send keeps all files and a retry uploads them again.
- Chat: two or more pictures form a grid as in Discord (`lib/fileGrid`); videos, audio and other files stay below. The viewer goes through the grid's visible pictures (buttons, arrow keys, swipe) and shows the description.
- Mumble: a text with several images is one message; web messages reach Mumble with a link for each file.
- Edit: as in Discord, files can be taken off, not added; an edit that leaves no text and no file asks to delete the message.
- Headless checks: a hidden tab (`visibilityState` hidden) stops painting pictures on purpose (`data-window-hidden`), so screenshots show empty cells until the mark is removed.

## Polls

As in Discord: a question (up to 300 characters), 2 to 10 answers (up to 55, each with an optional Unicode emoji), single or multiple choice, anonymous or named, 1 hour to 2 weeks. From the composer's "+" menu, for registered users (guest accounts too) in stored chats.

- A poll is a message with its own rows (`polls`, `poll_votes`, answers as JSON); its text (question and answers, "vote in Kallisti") is for Mumble, search, quotes and push. It goes through `send`, so slow mode, nonces, message rules and the Mumble mirror apply. The rules' word replacements reach the question and answers too, not only the text. Line breaks become spaces: they would fake answers in the text.
- The server keeps who voted, also in anonymous polls (one vote each, changes); it never sends their names. Each reader gets `mine` (its answers) where a message goes to one reader (history, pins, search, the `poll` event); other broadcasts leave it out and the client keeps what it knew. An ended poll never opens again in the client: a vote's counts can come after the end.
- Polls end in the server's minute loop (`EndDuePolls`, also after a restart; at most a minute late) or early by the author. The result is a no-ping reply by the author; `deliver` (shared with `post`) takes it to thread followers, direct chats and Mumble, where it comes from the server (`SendChannelText` from 0).
- Counts show after one's own vote, on "Show results", or at the end; shares are of the voters (multiple choice adds up to more than 100%).

- Follow-ups (user's decisions): answers take the server's custom emoji and personal ones, checked as reactions are (`usableEmoji`, shared); the form picks them with the emoji picker (inside the dialog: outside a modal dialog all is inert; Escape there closes only the picker). Polls ping nobody: a zero-width space after each "@" in the poll's text and its result (`quiet`) keeps them as they look, but no mention matches (push, @everyone, follows, the clients' highlights); the question itself stays as typed and shows as plain text.
- Headless note: the agent-browser daemon at some point stopped sending pointer and key input to new sessions (clicks report done, no events reach the page; `fill` still works); clicks from page scripts ran the same handlers.

## Astra review of files and polls

Fixed: a poll deleted while it ends could crash the server (the message read back without its poll; the minute loop runs in the server process). The bridge's text frame grew from 16 to 64 KiB: 10 descriptions of 1024 wide characters are a valid send of about 50 KB (a test builds the largest one). Waiting files are kept in IndexedDB while each file fits the limit (it compared the sum). On a one-file server a new pick replaces the waiting file again (the composer compared list lengths). Mumble's catch-up of direct messages has their files again (`MessagesAfter` loads them). Poll replies never ping. A poll form keeps its nonce for retries, cannot be closed while posting, and polls and file removal are offered only by servers that know them (`ready.polls`, `maxUploads` > 1). A description typed right before Send counts.

Left alone (the user's decision): an interrupted poll end loses its result, slow batches can outlive their upload tokens (5 minutes), crossing vote counts can leave older counts shown, Mumble's text size with 10 long file names, and a rollback after the upload migration (needs the deploy's backup).

## Streams after a reload; a quiet lost connection

- Watched streams come back after a reload or a lost connection: the rewatch memory (`lib/stream/rewatch`) lived in the Client, which is new after each reconnect and each reload. It now lives in App (a `StreamMemory`, per tab in sessionStorage, saved at pagehide, cleared at logout). The Client remembers only while both connections are up: a lost connection first closes the streaming socket, which drops what was watched (`gone` on the stream client, set before), and `follow` would then forget the names. The 30 s count from the loss (the moment the connection closes), not from the reconnect. Not tried with a real stream yet: the local server in the sandbox does not get the WHIP media (UDP on the LAN address).
- A lost connection stays quiet: nothing for 2 s, then a small grey "Reconnecting…", the banner with "Retry now" only after 10 s (`lossNotice`). While quiet the app tries each second; the growing waits (1, 2, 5 … s) start after it (`nextDelay`). Checked in a headless browser against a local server: a 5 s outage came back about a second after the server, with no banner.

## Voice CI: Safari stalls

24 of 100 voice workflow runs failed, recently nearly all in Safari. The usual failure: Safari sends nothing for over 300 ms in a spurt, then the waiting packets at once (frame numbers going on, the parts adding up to a normal spurt), and voicecheck counted the pause as a spurt end without terminator. Likely the main thread is busy for a moment on the slow CI machine: the worklet hands each 20 ms frame to it for encoding. voicecheck now counts such a pause up to 1 s as a stall inside the spurt and reports it; a longer one still ends the spurt, and the test's spurts are about 1.7 s apart, so a missing terminator between them still fails (they merge, and 4 spurts are expected). The other failure, Safari taking over 20 s to give the microphone, gets 45 s. Encoding off the main thread would remove the stall for real; not done (only if users hear gaps).

## iOS: export compliance

TestFlight 0.10.0.1 (`tauri ios build --export-method app-store-connect --build-number 1`). The upload was refused while Info.ios.plist said `ITSAppUsesNonExemptEncryption` true: with that, Apple wants the code of encryption documentation in App Store Connect (`ITSEncryptionExportComplianceCode`), and there is none. Without the key, App Store Connect asks per build, as for 0.10.0. The generated plist in gen/apple keeps a key that Info.ios.plist dropped (the build merges, it does not delete): remove it there too. For an App Store release the choice is still open: documentation, or an iOS app without the Mumble bridge (then only Apple's encryption, "false").

## Who reacted, and message info

A report: phones could not see who reacted (desktop shows it on hover only). As in Discord, a long press on a reaction opens a sheet with a button per emoji and the names (`ReactionsSheet`, `ReactionList`); as in Signal, "Info" (phone sheet, desktop hover actions) shows sent, edited and pinned times in the 12/24-hour setting (`lib/messageInfo`) and the reactions. The chip's long press comes as an untrusted contextmenu (`use:longPress`), a touch screen's own as a trusted one with hover none; a mouse right-click still opens the custom-emoji menu, and the sheet offers "Emoji options" for those emoji. Chips of users who cannot react are aria-disabled, not disabled: disabled buttons get no long press in some engines, and those users may see the names too. The sheet and the dialog follow the message by id (live updates; they close when it goes). Checked in a headless browser (the touch path with matchMedia faked): two accounts' reactions in the dialog and the sheet, a reaction taken back while the sheet was open.

## Stream sound drift after lost packets

During a TV show the sound drifted from the picture; reopening the stream fixed it, and the server's sync values were fine. Checked in headless Chrome: its AudioDecoder ignores the input timestamps and counts decoded samples from the first one (after a loss of packets 10-19, packets 20-29 came out at 200-380 ms, not 400-580 ms). So every lost packet moved the stream's sound 20 ms earlier against the picture, until a new decoder. The WebCodecs decoder now gives each output the timestamp of its packet, in order (`inputTimestamps`). Voice uses the same decoder; it plays chunks back to back, and its connection stats now see lost chunks (Chrome's counted timestamps hid them). Not tried on a live stream: the local server in the sandbox gets no WHIP media.

## Voice diagnostics: a speaker's sound

A user sounded muffled from the web client and fine from a Mumble client; nothing showed why. The built-in server now reads the first byte of each voice packet (the Opus TOC, RFC 6716 3.1) and keeps the last 250 packets per connection (about 5 s of speech): the band most of them had (narrow, medium, wide, super-wide, full), the last one's channels and frame length, and the bitrate of the Opus data. The admin view of connections (Server settings → Connections) shows it as "Sound", with a warning for narrow and medium band: often a Bluetooth headset whose microphone is in its phone mode. Kallisti itself sends full band at 40 kbps. Not checked in a browser: the local test setup has no admin with a speaker in voice.


## More diagnostics: microphone, stream drops, stream stats

- Microphone: the web client sends what the browser says about its microphone (name, sample rate, channels, which filters run) when it opens or changes; the connections view shows it, with a warning at 16 kHz or less (often a Bluetooth headset in its phone mode). The client sends it only when the text of it changes: the connection state is a new object with each server message, and the first version sent it again on each one (and used up the shared rate limit). Closing the microphone sends nothing: the view keeps the last device until the connection goes.
- Stream drops: the stream hub counts per viewer the pictures and sound frames it refused because the viewer's queue was full, and the pictures skipped while it waits for a keyframe ("Stream lost" in the view). Until now these drops left no trace.
- Stream stats: an info button on a stream tile shows its playout buffer, pictures and sound per second, bitrate, late pictures and sound, skips to a keyframe and gaps in the sound timestamps (lost packets; also a sender's pause up to 5 s). The rates use the real time between readings (hidden tabs run timers less often). Not tried on a real stream: no WHIP media in the sandbox, and headless Chrome cannot share a screen.

## The page scrolled away

A report (Chromium 156): after scrolling the messages or the member list, the whole app moved up, with an empty band below. The hidden texts for screen readers (`.sr-only`, `position: absolute`) had no positioned parent on wider screens: their containing block was the page, outside the lists' scroll areas. A "Muted" or "Deafened" in a scrolled channel list could sit below the window and make the document scrollable; a list at its end then scrolled the page. Reproduced headless (a 160 px high window: the document 211 px). Phones already had the guard; `.layout` is now `position: relative` and `overflow: clip` on all widths. No automatic test: the web tests have no layout (checked in the headless browser, before and after).

## The server's name and logo

Admins set both in Server settings → Server. The name is the root channel's (Mumble clients see the rename), stored in mumble_settings and preferred over `SERVER_NAME` at start; it is freer than a channel name (any text on one line, 100 characters), since the root is never looked up by name. The logo: the upload (token from the sidecar, admins only) becomes square PNGs in 32, 180, 192 and 512 px (its middle square, transparency kept), stored with a version from their content; `/api/logo/<size>.png?v=<version>` is cached for ever only at the current version (an old address gets the new picture, kept only until it changes). The server puts the name and the logo into index.html (title, icons) and manifest.json as it serves them, so an installed app and the first paint have them; the page changes the icons when the logo changes. That branding replaces exact strings of index.html: a test reads the web app's file. Installed apps take a new icon when the browser checks the manifest again, which may be later.

## Several servers, step 2: other sites connect with tokens

The web app on one Kallisti site will connect to other Kallisti servers in the same page (meta/issues/join-other-servers.md). Every server now accepts every origin, but other sites only with a token (as the apps did): CORS without credentials, and the cookie counts only without an Origin (navigations, a page's own GETs) or from the server's own origins. Not from a sibling subdomain: test.kallisti.chat and chat.kallisti.chat are the same site, so the browser sends the cookie there; a Sec-Fetch-Site check missed that (found in review). The page's CSP allows https:/wss: connections and https: pictures and media.

What that opened, and the answers (two independent reviews per commit, Claude and Astra, five rounds):
- Any website could make its visitors create guests or accounts: guests, new accounts and the fediverse signup only on the server's own pages and in the apps (Tauri origins, which websites cannot send). Account-less sockets from other sites are closed with a reason the client shows and does not retry.
- Password guessing spread over visitors' addresses: logins from other sites take a token per account name before the password work (parallel guesses count), give it back on success, unknown names and errors; a full table keeps every counting name and makes new ones wait, so nobody can reset a blocked name, and nobody outside can lock anyone out of the own site.
- Message HTML could track readers once the CSP allowed remote pictures: HTML profile only, no style elements, nothing that names an address outside link addresses and text.
- Sibling subdomains could plant cookies: `__Host-` names over HTTPS (users log in once again after this deploy). Older ones fixed on the way: a wrong fediverse callback no longer cancels a login, a link needs its session to last.
- Svelte's `$state` wraps objects in a proxy: a WeakSet of connections missed every one; connections are kept raw now.

## Several servers: the list on the home account

Step 1 first kept the added servers on the device, with an owner record (home login and session fingerprint) and many rules for stale tabs. All steps ship together, so the list moved to the home account early (part 1 of meta/issues/home-server-list.md): `/api/servers` (own site and apps, not guests, 100 per account, canonical origins of other servers) and a hub that loads it at start and when the page shows again. The device keeps only the tokens of the added servers, for one home login ("<home> <account>"); another login ends them (logged out there), a home logout too, the list stays. A new device shows the servers and asks for a login there (until step 5). About 240 lines fewer than the device list.

A rule from this work: no handlers for unlikely races that heal themselves (two tabs acting in the same instant, a short wrong display), only for destructive ones. So a cookie name per login (Chatto's ADR-017, against a late logout answer clearing a newer login's cookie) was written and dropped. Web Locks would not have made the device list safe anyway: localStorage writes are not guaranteed visible in another tab when it gets the lock.

Looked at Chatto and Fluxer (2026-10-27): neither has a home server across instances (Chatto: accounts per server, the list on the device; Fluxer: one big instance with guilds, about 20 services to host). Ideas kept for steps 3 and 4: short access tokens with rotating refresh tokens, a one-time authorization code bound to PKCE, push per server with its own key.

## Several servers: the rail outside the client

The rail was part of the client view: missing while a server connected, failed or asked for a login, and inert with the rest during a reconnect. App owns it now as a snippet: the client shows it in its left column (on phones in the channel drawer), the other screens beside them when there are servers to switch to. While a connection is lost only what acts on it takes no input (the chat's tools, list, hold-to-talk and composer, the side panels, the menus and dialogs); the rail and the phone's menu to the drawer stay usable. No automatic test (the web tests have no layout): checked in a headless browser with a server stopped, on desktop and phone widths.

## Several servers: voice while another server is on screen

The voice logic lives in the client view (microphone, mute, push-to-talk keys, the media session, Android's voice service, rejoining after a reconnect): switching servers unmounted it. Now the clients of all connected servers stay mounted, the others hidden; this also keeps their unread marks and notifications going. The one on screen alone has the page's title and icons, shortcuts and swipes, counts as seen (notifications, read marks), and a notification's click shows its server. Shared page state (title, icons, the media session, the Android voice service: one audio output for all) has an owner, so a hidden client's cleanup leaves another's. One voice channel across servers: the one that joined last stays, the others leave once and do not rejoin after a reconnect (leaving takes the server's answer: until then both are in voice, which first made the choice flip). The others show its voice bar; mute, deafen (one memory of the mute before deafening for all panels) and push-to-talk, also the phone's hold button, act on it; a lost connection's voice counts for nothing. Hidden textareas are not measured (no size), focus code looks only inside its own client.

## Several servers: Kallisti as a login provider, logins without typing

Every server answers the part of the Mastodon API that fediverse logins use, so another Kallisti server's existing fediverse login takes accounts from it unchanged (tested with our own fedi client against it). The design went through four review rounds:
- Apps were first stored per registration. Anyone may register, so the table could be filled, and deleting used apps broke other servers: our fedi client keeps an app once a login worked (found by the browser test, not a review). Now an app is its redirect URI (client id = its hash, nothing stored, the secret unchecked: PKCE and the exact redirect protect the code), and only another server's /api/fedi/callback, so no open redirect elsewhere on a site can take codes.
- The consent screen first parsed the query twice (preview and approval): tab characters made them read different requests. The approval now takes a ticket for the request shown.
- Hosts: instance hosts are canonical after IDNA (one root dot); admins' block and allow lists use the same form; a block covers subdomains.

Logins without typing (web): the add dialog opens the other server's page in a popup; its fediverse login runs as a handoff with the home server as provider: no cookie there (nobody can log a browser in by opening the popup), a session token made when the popup takes it, posted only to the provider's origin (the browser delivers nothing to another opener), taken by the dialog only from that popup. The popup page stays out of the account and server-list logic: it first ended another tab's added servers through the hub's owner check. Not for the desktop app yet (no popup: deep links).

## Several servers: invite links, push from added servers

Invite links (<server>/invite/<token>) work across servers: a click in any client on another server's invite shows that server (added first if needed: the dialog checks the address at once, with "Continue with your account") and redeems the invite in its client; the invite page of a server leads people to their home (?join=, the home address remembered by that site). ?join waits for the home account's list, and survives a login through another server (kept in tab storage for the next page). The web+kallisti: handler is left out: a page cannot tell whether it is registered, so its link could do nothing.

Push from added servers: each has its own service worker registration (/push/<origin>/: one subscription per registration, and each server has its own key), subscribed with its token while the home server's push is on. The worker learns its server from its scope. It must stay one file: importing the notice helpers from the page's push code too made the build share them as a chunk, and the worker failed to evaluate ("script evaluation failed"); a test now checks that only the worker imports them. Headless browsers have no push service (Chromium: "push service not available"; Playwright contexts are incognito, without the Push API), so delivery is checked on the test server only.

## Several servers: the final review

A review of the whole branch (Claude and Astra) found no reason to redesign, and nine points to fix before merging, mostly where the parts meet: an added server whose 30-day session ended was stuck (now the bridge closes a socket with an unknown token as "session expired", and the entry asks for a login again, the list unchanged); a home login ended elsewhere only closed the added servers (now their tokens are logged out once the home server says nobody is logged in); a failed handle lookup could get a known account past a block (the confirmed domain is kept and counts); push off left added servers' subscriptions of servers that were down; the remote voice bar hid the recording; long remote names did not fit the server list; a consent request got lost over a fediverse login; a voice invite on another server left the call. Later, as its own step: App.svelte (about 1000 lines) split into a few lib units with tests, one view for both client mounts, shared helpers.

## Several servers: cleanup after the final review

App.svelte had four coordinators in it; they are lib units with tests now (the popup login's phase, voice across servers, links from elsewhere, push of added servers), and one snippet mounts the home and the added servers' clients (added servers got the "Retry now" banner by that). Shared helpers replaced copies: selfOf (the own user, seven copies), apiWithToken, localStore (direct localStorage access threw where storage is blocked, in the login and fediverse forms and in push), one CORS function and one token maker on the server.

Blocking a home server now logs out the accounts that log in only with it (no password): sessions, live connections and stream keys, when the admin saves the lists. Accounts with a password keep their sessions: they can log in without the blocked server anyway. The settings list the servers an account allowed (Kallisti as a login provider), each to remove: the grant and its short tokens go, and the next login there asks again. Only on the own site, like the consent; the desktop app does not show it.

## Channel links, members from the menu, private categories

Requests from the community (2026-10-29). Voice channels have links (/voice/<id>): a question names the channel and the server first (a link on one server may lead to another), and says when it ends a call or takes voice from another device; invites that lead to voice ask too. "Copy link" on a private channel or category gives staff a one-use invite (7 days); its token comes with the answer to that request only (a shared listener had let a late token into the members dialog). Copying a link that only arrives later needs ClipboardItem with a promise in Safari (it copies only inside the click).

Private categories are Mumble ACLs: the category's channel gets the private entries for its subchannels only (ApplySubs, not ApplyHere). Mumble asks Traverse at every level: a closed category would hide a private channel in it from that channel's own members, and the category's level also needs an entry for each channel's own moderators. Members groups inherit only directly below a category; inheriting in the whole text tree would have let a "members" group SuperUser made on root into every private channel (found by both reviews). Moving a public channel out of a private category, or deleting a private category with channels, is refused: the history would become public.

## TestFlight 0.10.0 (0.10.0.2)

The main branch of 2026-10-29 (multi-server, channel links, private categories, quick reaction rows). `tauri ios build --export-method app-store-connect --build-number 2`; its own export fails without a local distribution certificate, so the upload is `xcodebuild -exportArchive` with destination upload and automatic (cloud) signing. "Failed to Use Accounts" means the Xcode sign-in expired (also outside the sandbox): sign in again in Xcode, Settings, Accounts. The build changes the generated Xcode project (team, build number): not committed.

## Android 7: Jackson and old WebViews

A user's Android 7 phone closed the app at once. Reproduced in the emulator (API 24, arm64): Tauri's Android library pins Jackson 2.15.3, and Jackson from 2.14 on needs Android 8: 2.15+ checks `instanceof BootstrapMethodError` at start (that class came with API 26), 2.14 calls `Constructor.getParameterCount()` (also API 26). The app's Gradle file forces Jackson 2.13.5, the last line for older Android; checked on Android 16 too (voice service, notifications through the plugins). Jackson only parses the app's own IPC there.

With that the app starts, but Android 7's original WebView (Chrome 52) cannot run the app's code (Vite's target is about Chrome 110); updated from the Play Store it is 119, enough. `public/old-browser.js`, a plain ES5 script before the module, says so instead of an empty page: without `Array.prototype.toSorted`, a message (in the app: update Android System WebView).

Emulator: `sdkmanager emulator "system-images;android-24;default;arm64-v8a"` with Homebrew's openjdk as JAVA_HOME, `emulator -avd <name> -no-window`; the release APK from `tauri android build` is unsigned locally, signed with the debug key via zipalign and apksigner.

## Coming back to the app

Two iOS videos showed the live page (no reload), then the reconnect banner, then an empty channel for a moment. Three causes: the socket closed in the background and the time hidden counted as lost (the banner at once); or it was dead without a close (noticed late); and the new connection replaced the old view before its text channels and messages came. Now `wake` starts the loss time again and tries at once (the first attempt of any loss goes at once), a connection that looks alive gets a ping (`checkAlive`: no answer in 2 s, it ends there, without waiting for a late close), and the session swaps in a new connection only when `readyToShow` (the session asks for the open channel's history itself), at the latest after 5 s.

Reproduced in Chromium: headless Chromium does not freeze a page through CDP (`Page.setWebLifecycleState` did not stop timers), so the test page holds due timeouts itself until a "thaw"; 300 ms latency through CDP network emulation.

## TestFlight 0.10.0 (0.10.0.3)

Main of 2026-10-30 (seamless return to the app, old-browser message). Same steps as 0.10.0.2; pnpm is no longer in the global mise config, so the build runs as `mise exec xcodegen@latest pnpm@10.33.4 -- npx tauri ios build --export-method app-store-connect --build-number 3`. This time the tauri export itself succeeded; the upload is still `xcodebuild -exportArchive` with destination upload.

## iOS without the Mumble bridge

App Store Connect asked about encryption for each build, because the bridge brings its own (rustls, AES-OCB2). The iOS build now leaves out the bridge, its modules and its crates (`cfg(not(target_os = "ios"))`; `cargo tree --target aarch64-apple-ios` has no rustls, ring or aes), so its networking is WebKit's alone and `ITSAppUsesNonExemptEncryption` is false in Info.ios.plist and the generated plist. `bridge_info` answers an error there; the web client asks once (`hasBridge`), names only Kallisti servers on the start screen, and refuses other addresses with a note. Plain Mumble servers stay on desktop and Android.

## Own channel list

A user hides text and voice channels and orders them for themselves (channel menu: Hide, Move up/down for me; "Hidden channels (N)" in the sidebar to show them again). Kept with the account on that server (`channel_lists`, one JSON per Mumble user id, sent in ready and to the account's other connections on change; 1000 ids per list at most), without an account on the device. The client applies it in one place (`ownView`: sidebar, channel menu, Alt+Up/Down); the open channel, the voice channel the user is in and channels with a mention stay shown; hidden channels and their threads show no unread marker; the quick switcher still finds them (after the shown ones). Saves drop ids of channels that are gone (a new channel can get a deleted one's id). An own move in a group fixes that group's order for the user: later admin reorders there do not show for them (no "reset" yet).

## Friends across servers

A friend is an account on one server (origin, user id there, the name last seen): the same person on two servers is two entries, because another server's tie to a home identity is only public if the user shares it. The list is kept with the home account (`/api/friends`, own site only, no guest accounts, 500 at most; the user id is required: none is not user 0); no friends without an account (no Friends button, no menu item), and guest accounts cannot be added (they go after a day: only members, which leave them out). "Add friend" in a user's menu; the Friends button at the top of the rail lists them, online ones first with their server and voice channel, from the servers in the rail only (nobody tells another server); Message is a `/dm/<user id>` link for that server's client. The home server takes part only as a Kallisti server (the desktop app with a plain Mumble server has no origin to keep friends by).

The reviews (Claude and Astra) found client races: the list now belongs to one owner (another login empties it at once; a change made for an earlier owner is not taken), a change ends loads still running, writes go in order, a refused one is told and the server's list comes back. A `/dm/` link opens a chat only with a known name (two unknown ones would share one chat's drafts), and only for 32-bit user ids. Lost connections count as offline.

Friends moved into the member list (the user's wish): a "Friends" group on top of every server's list, all friends from all servers (online first; another server's name and the voice channel below the name). A friend of this server opens the usual user menu; one elsewhere a small menu (Message on that server, or a note without a login there; Remove friend). The rail button and dialog are gone. All member groups collapse (heading buttons, kept on the device for all servers).

## GIFs as videos

Large uploads were mostly handled already (pictures over 1 MB or 1600 px get a WebP preview); the large ones still loading in the chat were GIFs (no preview: the animation). Measured on a week's 7 large GIFs: 32.8 MB as GIF, 14.7 MB as animated WebP at 640 px, 3.7 MB as MP4 at 640 px. So a large GIF (over 1 MB or 1600 px) gets an MP4 preview (H.264, CRF 26, at most 640 px, no sound), kept only if smaller; the chat shows it like the picture (muted, looping, no controls; loaded and played near the screen only, paused out of view; the GIF if playing is refused, as in iOS Low Power Mode), the lightbox and downloads have the GIF. The review found three things: `-enc_time_base 1/100` (else ffmpeg guesses a frame rate from GIF delays, and frames of mixed lengths are dropped and moved: a test GIF of 2.57 s came out as 3 s), lazy loading (every GIF video of the history loaded at once), and transparency (an MP4 has none: GIFs that show it, by the first frame or a frame cleared to the background, stay GIFs; read from the block headers only). Production's ffmpeg is 6.1.2 with libx264; the start-up log says gif_videos.

## TestFlight 0.11.0 (0.11.0.5)

Main of 2026-10-31: no Mumble bridge on iOS (ITSAppUsesNonExemptEncryption false: no export compliance question), own channel list, friends in the member list, GIF videos. The Xcode sign-in had to be redone (Xcode → Settings → Accounts: "Failed to Use Accounts", "missing Xcode-Username" in the keychain); afterwards both the tauri export and `xcodebuild -exportArchive` with destination upload worked.

## Tiny picture grids

bajax saw two pictures as tiny squares. Reproduced in Chromium and WebKit: a message body is a flex item that sizes to its content, so the grid's `min(520px, 100%)` resolved against the content. With a short text (or none) Chromium made the grid 207 px wide; WebKit 210 px until the pictures loaded. Earlier tries used a long link as the text, which widened the body and hid the bug. Now `width: 520px; max-width: 100%`. Letting the body fill the line also fixed it, but stretched link previews and spoiler pictures to their maximum width.

## Webhooks

A secret URL posts into a text channel: `POST /api/webhooks/{id}/{token}`, Discord's path and format, so tools with a "Discord webhook" setting (Gitea, GitHub Actions, Grafana, Uptime Kuma) work with the URL alone. The user's choices: admins and moderators manage them (channel menu → Webhooks…), Discord's format (Slack's `text` too), a post may set the name but not the picture (a picture URL would make every reader load a foreign image; the webhook has its own, uploaded by staff), text channels only.

- The token is 192 random bits; the store keeps its SHA-256 (as invite links), so the URL shows once. At most 10 per channel; a hidden channel loses its webhooks (also through EnsureChannels), so one that comes back has no old URLs. Ids are AUTOINCREMENT (a stale delete must not hit a new one).
- The post: JSON, or multipart (payload_json, or content/username fields; files ignored). content/text, then embeds as lines: author, **title** with its URL, the description quoted, **field**: value, footer; Discord's masked links `[text](url)` become `text (url)` (our Markdown has none; the client links the URL). Content over 2000 characters: 400 (as Discord); the whole text is cut at 2000. Errors are Discord's (`{"message","code"}`: 10015, 50027, 50006, 50035, 50109; 429 with Retry-After and retry_after). `?wait=true` answers with the message.
- No pings: a zero-width space after each "@" that could start a mention (the boundary of mentionPattern and the clients), so URLs with "@" stay whole.
- Limits: 5 posts per 2 s per webhook (a token bucket), checked before the body is read; 64 KB of JSON, 10 embeds; 30 s to read the body; the token is checked again before storing (a webhook deleted meanwhile posts nothing).
- A post's name may be anyone's: replies to it ping nobody (the quote carries `webhook`; server and client), it never groups with a user of that name, nobody owns it (no edit; moderators delete), the App tag shows, and the avatar is the webhook's picture (or initials), never a user's by name. Mumble users see "Name (webhook): text" (live, from the server) and the marked name in their history.
- Pictures: staff upload one per webhook (a 128 px square, as the logo; its own token kind, not accepted as a file upload token); served without a date (one replaced within the second would revalidate as the same) and announced to everyone (`webhook-picture`, a version for the address), so open chats load it anew.
- The second Astra round found: the read deadline came after early answers (Go reads a body before answering 429), the last check and the insert were apart (now in the insert's transaction), Mumble reply quotes lacked the marker, trailing JSON past the limit, URLs with ",@" quieted (now URLs are skipped).
- Reviews: Claude (server, web) and Astra (twice). Checked in Chromium with Playwright: create, the URL once, a picture, a Discord post with an embed, the App tag and picture for another user, no @everyone ping, rename, delete, 404 afterwards.
## Log in with a QR code

One device logs another in by a code, so nobody types a password on a phone. The user's choices: both directions, the logged-in device always confirms, the code works in the app's scanner and with the phone's camera.

- The code is a link `<server>/qr/<token>` (the camera opens it in the browser; the SPA answers the path), valid 2 minutes, scanned once. An offer: a logged-in device shows it (Settings → Log in on another device), a logged-out one scans it, the owner answers "Log in on <device>?", the scanner takes its session. A request: a logged-out device shows it (login screen), a logged-in phone scans it and answers "Log in on <device> as <name>?" with a phishing warning, the showing device takes its session.
- Server (auth/qr.go): in memory, at most 10,000 codes; start under the login rate limit; each step needs its own secret (token in the code, owner secret, claim secret). The session is created only while the approving session is valid, in one statement (store.CreateSessionVouched): a logout everywhere or a new password ends codes not taken yet. A device with a session neither scans nor takes an offer (409): a stranger's code cannot move a logged-in device into the stranger's account. Guests neither give nor get one. Device names: at most 64 characters, no control or format characters (bidi overrides would forge the question). The start answer calls the code "code": the client keeps any "token" as its session.
- Client: the code is drawn from uqr's matrix as one SVG path (dark on light, whatever the theme); Chromium's BarcodeDetector reads it. One polling helper with a deadline; one dialog frame.
- Apps: tauri-plugin-barcode-scanner on Android and iOS (camera permission; NSCameraUsageDescription). "Scan a QR code" on the login screen (also before a server is chosen: the code names it) and in Settings. The Android debug build passes; the scanner itself is for the user to check on a phone.
- The second Astra round found: no byte limit on the QR bodies (ServeHTTP only sets a deadline), "done" shown before the session existed (now a hidden "taking" state, then done or refused), a hung poll outliving the deadline (raced against it), dialogs closed during their first request still starting (guarded), a QR login finishing after a server change (guarded as the normal login).
- Left on purpose: a poll in flight when its dialog closes can still install its session (the user would have to log in otherwise within that second).
- Checked with Playwright in Chromium, two browsers: both directions, and no account switch for a logged-in device. Reviews: Claude, Astra (two rounds).

## TestFlight 0.11.0 (0.11.0.6)

Main of 2026-11-01: webhooks, QR login with the camera scanner (tauri-plugin-barcode-scanner; NSCameraUsageDescription in the Info.plist), the picture grid fix. The Android APK of the same commit came from GitHub Actions (android.yml builds every push to main that touches the apps).

## Settings in categories

The settings were one long page. The user chose layout A of three HTML mockups (ops/mockups/settings.html): on desktop a side menu (User settings: Profile, Account; App settings: Voice & video, Notifications, Appearance; Log out at its foot) beside the open category; on phones a list (with "Edit profile ›" on top), each category its own page with "‹ Settings" (Escape goes back to the list first). Empty categories do not show (no account: no Account); the reopen after a fediverse confirmation and a guest's "keep this account" open at Account. The markup blocks moved unchanged; drafts that are dialog state (bio, password fields) survive a category change.

- app.css gives dialog headers a sticky -24 px margin (for padded dialogs) and modals a safe-area inset: this dialog has no padding and fills a phone's screen, so it sets both back (inset 0, its own safe-area padding).
- Review fixes: the push-to-talk key recording ends with the Voice category (as the echo test), focus moves to a phone page's title and back to its list item, phone pages show their title, guests log out on Account only (where the warning is), a category that goes (Profile while reconnecting) falls back to the first.

## Server settings in categories

The server settings had ten tabs in a row. Now they use the user settings' layout: Server (Overview: name and logo; Emoji; Stickers; Threads; Message rules), Members (Registered users; Bans; Guests), Monitoring (Connections; Recordings), each only for who may use it, as the tabs were. The frame (header, menu, phone pages, back and Escape, focus, the full-screen phone dialog, app.css's dialog rules undone) is one generic component, CategoryDialog (lib/dialogCategories.ts; lib/categories.ts is the text channels' groups); both settings dialogs fill its content, menuTop and menuEnd snippets. Phones show the group headings too (ten items in one list were hard to scan).

## Add a server with the linked fediverse account

An account named after its fediverse home (Lars@shitposter.world) could not add another Kallisti server with "+": its home server's login provider refuses such accounts (else names like Lars@shitposter.world@chat.kallisti.chat), so the popup failed. Astra and GLM-5.3 each looked at the options and recommended the same: the popup on the other server logs in with the linked fediverse account itself, not through the home server. That server finds an account it already has for that fediverse account (also one with a local name), or makes one with the same name; its blocks of fediverse servers apply as for any login. They rejected the home server vouching for the fediverse account (it could claim any) and nested names. A fixed handle with a separate display name is its own issue (handle-and-display-name).

- The session used to go to the server that did the login (it vouched for the account). Now it goes to the home's page, an address in the popup link, which any site can open; the instance may not ask the user again. So that server asks on its own page first ("Allow <home>? It can then read and write here as you."), and gives the token only for the origin asked about (POST /api/fedi/handoff: first {ask, name, origin}, then {allow: origin}). The old path through the home server asks nothing.
- Review fixes (Claude, Astra): the token was no longer taken once under parallel requests (a look, then a take whose result was dropped; also on the old path); a yes counted for any login waiting in the browser (another login could replace the one shown: now the question has a key, and the yes names it); Allow works after 0.6 s only (the opener decides when the popup shows: no double click lands on it); a fresh sign-up knew no fediverse server until a reload; a home on another port could not ask. Left out: a reload of the popup during the question loses it (close it and start again).
- /api/me names the linked account's server (the stored API host, which may differ from the handle's domain). Linked local accounts get "Continue as @handle instead" below the usual button.
- Checked headless (two local servers, the fake instance, Chromium without media devices): sign-up on A, "+" with only the fediverse way, the question on B, B in the rail as Lars@127.0.0.1:18099. Two local-only workarounds in the test: the CSP lets pages reach other servers over HTTPS only (bypassed), and a scheme-less host means HTTPS (the fake instance is plain HTTP).

## Names with one character

Account names have 1 to 32 characters now (were 2 to 32), for web accounts and Mumble registrations alike (names.Check); names from other servers already allowed one character before the "@". A fediverse login suggests a one-character remote name as it is.

## Older messages load while scrolling up

"Load older messages" is gone from sight: a page loads by itself when the reader comes within 1.5 screens of the top (lib/scroll's nearTop), so it is mostly there before the reader; again after a page that leaves the reader that close (tall screens, short pages). Pages have 50 messages (were 30; 50 of 2000 characters are far below the 8 MB frame limit). The top line says "Loading older messages…"; the button stays for the keyboard, shown only with the focus.

- Checked headless with 300 seeded messages: 50 at first, five pages while scrolling up, the position kept (a wheel of 700 px and a page of 1300 px above: the view moved by exactly the page), the channel's start at the end.
- Review fixes: the reload check runs after the scroll effect (else it saw the list before its scroll to the bottom and loaded a page at every first open); one waiting page per channel (with one slot, a thread pane and the main pane near the top took it from each other in a loop of requests); a page that gets no answer, or an error like the last one, ends after 10 s, so scrolling asks again (the button used to be the retry).
- iOS (the user: "jumpy as soon as it loads"), reproduced in the iOS 27 simulator (XCUITest drags, a screen recording, frame-to-frame differences): a page that came in during a fling lost the position; iOS keeps the fling going from the old offset and drops the one set for the page, so the reader landed at the top, the screen went blank for a few frames and every page loaded after it (message 311 to 61 to 9). Now a page that only adds older messages (lib/scroll's olderOnly) waits while a touch moves the list (a finger on it, or a scroll in the last 150 ms) and goes in when it stops; with the 1.5-screen window it is mostly in before the reader gets there. A wheel, a mouse or keys add it at once (Chrome was fine). CSS.supports("overflow-anchor: auto") is no test for this: iOS 27 claims it and still drops the position. Checked again: no blank frames over 14 swipes in the simulator, Chromium unchanged, channel switches still open at the bottom.

## Long names on phones

A name like minazukimidnight@diclonius.princess.wedding made the message header wider than a phone (a flex row without wrapping, a name without break points): the time went off screen and the whole list panned sideways (the user's screenshots). Now the header wraps (the time to the next line, the name breaks if it is wider than the line), the time stays on one line, a reply's name is cut with an ellipsis, and the list never scrolls sideways. Reproduced and checked in the iOS 27 simulator.

## Realistic test channels

For tests in a realistic setting (the user's wish), `server/cmd/seedchat` fills a stopped server's database through the store package: a category "Community" with off-topic, gaming, music, photos and dev-talk, 220 to 340 messages each over 30 days (more in the evening, few at night, in bursts), about twenty people (two guests, two with other servers' names), replies, mentions, reactions, pictures (one or several; the server makes previews at its next start), six polls with votes, nine threads with 10 to 40 messages, pins, code and links. The seeded people have Mumble ids from 900001 on, so nothing belongs to a real account; existing channels are left alone. Run on test.kallisti.chat (photos from picsum.photos, two generated GIFs; the database copied to backups/before-seed-20261102.db first).

## The iOS web app moved as a whole

In the installed web app (not in a Safari tab), a swipe past the end of the chat list moved the whole app: the header off the top, a gap below the message box (the user's recording). iOS passed the swipe on to the page, and the page bounced. Reproduced in the iOS 27 simulator with the app added to the home screen. Now the page never bounces (overscroll-behavior: none on html and body) and the chat list keeps its end to itself (overscroll-behavior-y: contain); the list still bounces inside. Checked in the simulator and by the user on test.kallisti.chat.

## Outside PRs: composer, audio recovery, uploads storage

Three GitHub PRs, merged by the user after review:

- #8 (hutchisr): on desktop the message box has the user panel's height and bottom edge; "is typing…" floats above the box instead of a fixed row below it, and "Jump to present" moves up while someone types. Phones keep the reserved row. Checked with screenshots of main and the PR (1280 px, a second guest typing).
- #9 (hutchisr): an interrupted audio context (WebKit, also the Mac app) now resumes in a visible window too, at once when voice starts, and a click resumes it like a suspended one. Suspended contexts stay with the click handler (user activation, the Linux app's worklet hold).
- #12 (cortexobscura): commented examples for uploads on their own disk (a Compose bind mount, a Kubernetes PVC). Safe: uploads are written into the upload folder directly, previews are renamed only inside it, so no rename crosses file systems.

## Invite-only registration (PR #11)

From cortexobscura's PR #11, finished here. An admin switch (Server settings → Registration, off by default and after upgrades) closes signup: a new account needs an account invitation link (`/register#token`, the token in the fragment so it is in no request or Referer; only its hash is stored), new guests are refused on the web and over Mumble. Existing accounts, sessions and registered Mumble users keep their access; a guest from before can still get a password (the user's decision). The policy check, the invitation's use and the account's insert are one transaction (one store connection), so a failed insert gives the use back and twelve signups at once get one account from a single-use link. The policy is a store table, not mumbled meta like guests-read-only and captcha, because it must be in that transaction.

Review fixes on top (a subagent and Astra reviewed the PR):

- Invitation ids are AUTOINCREMENT: SQLite used a deleted highest id again, and another admin's stale revoke deleted the new link.
- One admission function for the early check (before the password hash) and the transaction; with registration open an invitation is neither checked nor used (an old link gave 403 before). One CreateAccount and one CreateFediAccount, both with the invitation; every creation passes the policy.
- An invitation carried over a fediverse login has its own tab key; it took the one slot of a waiting channel link, ?join or ?authorize.
- Second review round: an invite_required answer shows the invitation field (the policy can be unknown: an older server, a failed read; the Guest tab then goes too); a pasted token is cleared when the desktop app changes to another server (a bare token says nothing about its server); a kept invitation is taken even when the address brings a newer one.
- Docs: bots over Mumble (Jukebox, Kallisti TV) are guests unless their certificate is registered; register them before the switch.
- Left as they are (they heal or are harmless): a Mumble guest that registers just as the switch goes on, an old snapshot that shows the old switch state, an invitation that expires while its signup waits on the database.
## Three pictures: a gap in the grid

Three pictures show as one big cell (two rows high) and two small ones. The big cell had no aspect ratio, so its picture's own height sized the rows: a portrait first picture (1080×1920) made the grid 613 px high instead of 345, and the two small pictures sat at the top of their rows with a gap between (the user's screenshot on test). Now the big cell has `contain: size`: the small cells set the rows, the big picture is cropped to them. Measured headless in Chromium before and after (a seeded message with a portrait picture first); no unit test, it is layout only. Firefox and WebKit did not start on the Mac; `contain: size` is in both for years.

## OBS: how to turn off B-frames

A user's OBS 30.1.2 stream (x264, profile baseline) showed a white window as pink with a purple bar until they put `bframes=0` in "x264 Options". The dialog said only "no B-frames", and OBS's x264 has no field for them. Now the dialog and the docs say it per encoder: x264 `bframes=0` in "x264 Options"; NVENC, QuickSync, AMF: B-Frames 0. The server still forwards the video as it comes.

## OBS: the server tells the streamer about B-frames

The server forwards OBS's H.264 unchanged, and viewers decode pictures in the order they come, so B-frames break the picture (a user saw pink with a purple bar). Now the WHIP code reads each non-IDR slice's slice_type (two Exp-Golomb codes after the NAL header; 1 or 6 is B). At the first B picture of a stream it logs it and the owner's app shows for about 14 s how to turn them off (x264: bframes=0 in x264 Options; NVENC, QuickSync, AMF: B-Frames 0), once per stream. The way there: whip.Handler.Warn → sidecar Hub.Notice (a "notice" to that voice session's connection) → the notice toast. Toasts now stay 0.3 s a word (at least 5 s), so a long text can be read. Tested with hand-made slice headers, a real x264 clip with bframes=2 (testdata/bframes.h264, 6 KB) and the clip without them.

## Unread line, jump to unread, mark unread

A user's request, Discord's three features. When a chat comes into view (opened, or the tab visible again) with unread messages of others, a red "New" line sits above the first one; it stays while the chat is open (messages that come in while it is watched do not move it) and goes when the chat closes, on "Mark as read", or when we send. None without an earlier read mark (a first visit, a guest's first look). While the line is above the view (or not loaded yet), a bar says "N new messages since today at 3:27 PM" (N+ while the first is not loaded); Jump pages back as other jumps do (jumpStep, ten pages at most). "Mark unread" (the message actions, the phone sheet, Alt+click) sets the read mark to the message before (message ids count across chats, so "unread from M" is "read up to M−1"); the chat stays unread while open (the view holds it at that mark) and the line moves there. For accounts the server keeps it: a new "mark-unread" request, the only way a mark goes back (store.ResetReadMark), and a "read" event with back:true, so the account's other sessions lower their own seen marks instead of sending the old one again. Review fixes: another session of the account that has the chat open holds it too (else watching it read it again and sent the old mark), Jump stops when the chat changes or is read meanwhile, Alt+click spares links and buttons. Checked headless with an account and a guest: the line above the first of 30 new messages, the bar, Jump, Alt+click moved the line and the chat stayed unread while open, and after a reload the server's mark put the line there again; with two tabs of one account both showed the line and a fresh tab found the mark kept.

## nosniff on every response (PR #13)

From cortexobscura's PR #13, for finding 2 of the security audit in #10: a wrapper around the mux sets `X-Content-Type-Options: nosniff` on every response (the API, OAuth, redirects, WebSocket upgrades), with the original writer passed on, so streaming and upgrades work. Pages, assets, uploads, emoji, avatars and previews had it already, and our bundled Caddyfiles set it too, but chat.kallisti.chat sits behind another Caddy and `/api/server` had none.

## Security audit #10

SHSauler's audit (GitHub #10) found nothing exploitable; hardening items, worked through:

- CI: every action is pinned to a full commit SHA (the version in a comment); `.github/dependabot.yml` sends one grouped update a week. helm, voice and test had no permissions block: the repository default (read) applied, but scanners count that as write-all; now `contents: read`. desktop and android keep `contents: write` (they make the release).
- Dependabot's first grouped update (#14): 18 actions to new majors (mostly the Node 24 runtime; tauri-action v1 names the macOS .app.tar.gz with the version). Release notes checked against our use; all workflows green on main, also image, site, android and desktop, which pull requests do not run; the Safari voice check needed a rerun (a runner timeout).
- Helper images: health checks (Caddy DNS-01: its admin endpoint on localhost; cert-sync: a synced certificate exists, 5 minutes to start). No non-root USER: in Compose, Caddy runs as root and its storage (with the certificate, mode 0600) belongs to root, and cert-sync must read it and hand the copy to the app's user; a USER would break the certificate sync of every existing install. Kubernetes runs both as 1000 already. Built and checked with podman on the NAS.
- nosniff on all responses: PR #13.
- Confirmed: production ALLOWED_ORIGINS is exactly https://chat.kallisti.chat (other origins get Allow-Origin for token calls, never credentials); session cookies are Secure with the __Host- prefix when a trusted proxy sets X-Forwarded-Proto to https (the shipped web listener serves plain HTTP); SHA-1 is the Mumble protocol's (certificate and blob hashes); math/rand picks replacement words and runs in dev tools only; the exported Android activity is the launcher (required); glib 0.18 and proc-macro-error come with Tauri's gtk 0.18 on Linux and wait for Tauri.

## Helper-container capabilities and certificate-sync health

Keep the Compose helpers' existing users and volume ownership: the copier needs to read Caddy's private key and publish it for an arbitrary app PUID:PGID. Drop all capabilities, then allow only NET_BIND_SERVICE and DAC_OVERRIDE for Caddy, and CHOWN, DAC_OVERRIDE and FOWNER for the copier. The latter needs all three even when updating an existing private certificate directory. Kubernetes already applies its own non-root security context.

Replace the copier's certificate-file-exists healthcheck with the result and heartbeat of its last completed pass. Waiting for initial issuance is healthy; rejected material and stalled passes are not. Clear previous status at startup, keep the last valid certificate on validation failure, and preserve atomic renewal. Tests cover custom ownership, restricted capabilities, non-root operation, custom paths, health transitions and interrupted publication. DNS-01 tests use a disposable image tag and an offline container with private synthetic token/configuration files, without touching deployment images or requesting real certificates.

## HTTPS proxy configuration warning

Follow-up to audit #10, findings 4 and 5: warn at startup when an HTTPS allowed origin is configured without TRUST_FORWARDED_FOR=1. That setting also controls trust of X-Forwarded-Proto; without it, the plain HTTP web listener cannot recognize HTTPS at the proxy and login cookies lack Secure and the __Host- prefix. The warning does not reject configuration or enable trust automatically. Keep the existing wildcard warning and document the proxy and app-port isolation requirements.

Regression tests cover the warning conditions, an untrusted HTTPS header, plain HTTP, missing or HTTP headers from a trusted proxy, and the fediverse state cookie over trusted HTTPS. Cookie behavior, existing sessions, and the legacy-cookie migration deadline are unchanged.

## Moving from Murmur: import-murmur

`kallisti import-murmur -from murmur.sqlite` takes a Murmur database (schema 9, Mumble 1.4/1.5) into a new Kallisti: channels (names, descriptions, positions, links, ACL inheritance), ACLs, groups with members, registered users (name, certificate hash, comment, avatar, last channel), bans, the server name. Murmur's permission bits are Mumble's, as ours, so ACLs copy as they are. Three parts: `internal/murmur` reads one virtual server (Murmur's ids) and plans the import; `mumbled.DB.Import` writes it in one transaction into a fresh server (no registered users; no channels but root, the text mirrors, which stay, and the lobby's automatic General, which goes), the channels after the existing ids; the subcommand checks, takes a backup (backup.Run) and runs it, with -dry-run to only see the plan.

A friend's database (the user's) had two virtual servers. Default: each becomes a channel under root, its root ACL and groups there (its admins manage that part; Kick, Ban and Register work only in root, said so); users are numbered anew, one certificate is one user (memberships of both), a name an earlier server's user has gets "-2". With -server ID (or one server only): that server is the whole Kallisti, its root ACL and groups ours (its admins are Kallisti's), plus the lobby rule (the user's choice: otherwise a web user was in voice in root right after login). Not imported, and listed: passwords (only SuperUser has one in Kallisti; 3 of 106 users had one, four of the hashes the old unsalted SHA-1), users without a certificate, emails, user limits, listeners, the welcome text (MUMBLE_WELCOME), the default channel. -cert writes Murmur's own certificate and key to CERT_DIR (checked first, key 0600, old ones kept): the Mumble port then served the same certificate (SHA-256 fingerprints compared), so clients see no warning.

Review fixes: root's links went through the id offset too late (with a server started once, root linked to a text mirror); ban starts with a Z (Murmur writes UTC) were taken as now, so timed bans got longer; -cert refuses when MUMBLE_CERT_FILE is set (the server would not use the files) and writes both files before it renames; the name SuperUser and names with an @ (an account of another server here) get a suffix or a dash; one certificate is one user also within one server; a channel whose parent is gone goes under its server. Checked with the real database in temporary folders, both ways, Kallisti started once before: 25 channels and 106 users (one renamed) under two top channels, the mirrors kept, avatars shown; alone, server 1's 17 admins were Kallisti admins. Registered Mumble users still cannot log in on the web with their name (no password): an old gap, not the import's.

## Unregister deletes the web account

A user's report: after "Unregister" in Server settings → Users they still logged in with their name and password. A web account has two records: the account (name, password, in the store) and its Mumble registration (id, roles, ACL entries; the web app is a Mumble client with the account's certificate). The split comes from the start beside Murmur, whose user table is not ours. Unregister removed only the registration, and the next login registered the account again with a new id.

Unregister without deleting the account had no use: the account came back without its roles. Unregister is Mumble's delete (a native user's registration is the whole user), so the users list has one action now, "Delete user…" (the user's choice). For a web account (its name and certificate) it deletes the account first (sessions cascade, open connections, the stream key and Mumble password logins end), then the registration; if the account cannot be deleted, both stay. Only admins (Write in root) delete users, and only they see the list; moderators kick, ban and time out. Admins are deleted only by SuperUser, as only SuperUser removes the admin role (the user's choice). Rename is gone from the list (the server refused it for accounts; a fixed handle and display name will come); native admins can still rename Mumble-only registrations. Messages stay under the old name; Mumble user ids are never reused, so a new account with the name gets no old DMs, status or emoji.

Astra's review: account ids were reused after a deletion (SQLite gives max+1). Other Kallisti servers know an account by its id (the login provider), so a new account could have opened a deleted one's account there. Ids now come from store_meta last_account_id, like message and Mumble user ids. Before, only guest accounts were deleted, and they cannot use the provider. Astra's second review (the final state): the target of a deletion was a user made of its id only, so an admin by a certificate or token ACL did not count as an admin, and another admin could delete them; the target is now the online user, else the id with the registration's certificate. The account counter started at the first new account, so on an existing database the newest account deleted before that still gave its id again; Open now sets it from the accounts there. A failed account lookup made the deletion a plain unregister (the account stayed); now the deletion callback looks up the account by name and certificate itself, and a database error stops it. The users list still offers Delete for admins by ACL only (not in the admin group); the server refuses it. Left as they are (unlikely timing, or older): a native login that is half through while its account is deleted, a fediverse avatar download that ends after the deletion and registers the name again, a crash between the two deletions (Delete user again removes the rest), upload tokens that live five minutes.

## Localization (branch i18n)

Users asked for Kallisti in their languages. The client (web, desktop and Android share it) now has English, German, French, Spanish, Japanese, Simplified and Traditional Chinese. The browser's language is the default; Settings → Appearance → Language overrides it per device (the user's choice). Our own small module, no dependency: English dictionaries in TypeScript (src/i18n/en, one file per area) are the source and the key type of t(); other languages may leave keys out (English then); plurals with Intl.PluralRules (by "count"); a sentence with an element inside keeps its placeholder (around()). Other languages load on demand (one chunk each, about 70 kB); main.ts awaits the chosen one before the app mounts.

Seven subagents moved about 1200 texts into the dictionaries, by area, each owning its files; six translated (machine-made, for review by native speakers in the PR; informal du/tu/tú as Discord). A test checks that each language has every key with the same placeholders and plural forms. English output is unchanged but for plurals ("1 mention", "1 day"). Dates and numbers follow the language in use, with the browser's region when it has that language (en-GB keeps day/month); with "auto" and none of our languages, the browser's formats as before. Texts the server sends stay English (refusals, errors, notices): a follow-up issue (codes translated in the client). Also English: emoji names and search (emojibase data), the service worker's fallback text.

Review fixes: a slow earlier language load could win over the latest choice (now a counter); the setting store is made on first use (node tests warned about localStorage); a few client-made errors were still English (not connected, upload, streaming, push); the spoiler label went into a replace() pattern; two focus lookups searched by translated aria-label (now classes). Japanese broke lines inside words in narrow tabs: word-break: auto-phrase for :lang(ja) (Chrome). Checked in headless Chromium against a local server in all seven languages (login, client, settings).

## Threads in the sidebar as Discord

A user: a closed thread stayed in the sidebar ("imagine having 20 threads"). The sidebar showed every active thread of a channel, the ones not followed only dimmed; the ✕ closes the view only. Now, as Discord: the threads we follow and the open one. Following was already as Discord's joining (starting a thread, writing in it, being mentioned or answered there). The menu says "Join thread" / "Leave thread" now (all languages). The others: the channel's thread list in the header and the quick switcher. Guests cannot follow threads: they still see all active ones.

## Apps: voice over UDP on Kallisti servers

A user's log: an Android app at 420 ms ping with 15.4 % of the audio to it late or lost, over `transport=websocket`. On Kallisti servers the apps sent voice inside the WebSocket (TCP), where one lost packet holds back all after it. For Mumble servers the app's bridge already used UDP. Now Kallisti connections in the desktop and Android apps go through the bridge too: it opens the server's WebSocket (wss with the usual certificate authorities, the web view's Origin and subprotocols with the session token), passes text and the Mumble stream through, and sends voice over UDP to the Mumble port on the WebSocket's host once pings come back. The server needed no change for that (web sessions already got CryptSetup and were found by IP and key; a test says so now): only MUMBLE_PUBLIC_PORT (default the listen port, 0 none) as udpPort in /api/server, and the transport "websocket+udp" in the connection list.

Servers send voice over UDP only to clients that sent voice over UDP (as Murmur), so a listener heard over TCP. The bridge sends an empty voice packet to the loopback target after each pong until voice comes over UDP: the server switches the client to UDP and drops the packet (a tested contract in voice.go); not to Murmur, which echoes the loopback. Checked end to end with the real server and bridge and two bot clients: both directions over UDP (100 of 100 frames), with UDP blocked voice stayed in the WebSocket (100 of 100). Not checked with packet loss (macOS needs root for it).

Reviews: the bridge lost the server's close reason (the client decides on "logged out" and the like: a reconnect loop instead of a new login); a blocking DNS lookup; Murmur and the empty packet; one lost announce kept a listener on TCP; private CAs and system proxies (the bridge has the web PKI only): such a server now goes straight to the WebSocket after "cannot reach"; the UDP port is asked once, at most 5 s. Astra: through the bridge the client took its HTTP address from the bridge URL (uploads, screen sharing, pictures would have gone to 127.0.0.1): the target carries the server's site now; a server's message full of tiny frames became millions of frames at once (now in 32 KiB pieces); voice from the server kept UDP "usable" although only the way down worked (now only pongs). Astra's follow-up: a late voice packet from before a UDP outage counted as "heard" and kept a listener on TCP after it; the server's close could still be cancelled when the client side ended first (now it gets 2 s); a long "cannot reach" reason (TLS errors) made an invalid close frame (at most 123 bytes now, the rest in the log); a first connect that the bridge could not make did not try straight by itself (shouldReconnect allows that one now). Left: Mumble's own UDP rebinding and endpoint quirks (older than this), a UDP host other than the web host (a CDN in front: voice stays in the WebSocket).

## Android: the QR scanner crashed the release app

A user (Galaxy A34): "Scan a QR code" opened the camera, then the app closed. Reproduced with the v0.13.0 APK in the emulator: a NullPointerException in ML Kit's logger, from `InputImage.fromMediaImage` on the first camera frame. Release builds run R8, and R8 broke ML Kit's internals; debug builds (where the scanner was tried) do not. Keep rules for ML Kit in proguard-rules.pro: a local release build opens the camera without a crash. Not checked in the emulator: reading a code (its virtual camera does not turn to the poster, and the barcode model comes from Play services first).

## Private-room media (PR #18, cortexobscura)

`EXPERIMENTAL_PRIVATE_ROOM_MEDIA=1` (off by default, built-in server only): a room's files follow the room's access, as its chat. cortexobscura's PR gated `/files/` by the session's account and the room's ACL. Reviews (a subagent, Astra): a deny for the account's certificate did not count (the reader had no hash); the gate copied the chat rules and differed (voice chats need MayRead as the chat, text channels without a mirror are public); the server's link-image cache kept a copy after the room became private; the apps and other servers' tabs send no cookie with images, so all private files broke there; each file request loaded the whole message and looked up the session even for public rooms.

Now as Discord: messages carry signed file links (HMAC with a key kept in the new secrets table, `ex` rounded up to the hour, so a link works 24 to 25 hours and stays the same for an hour of caching). A signed request checks only the signature, so apps, other servers' tabs and Mumble clients load private files, and no room lookup happens per picture. The open chat asks for fresh links (file-links request, answered as edits) when they expire within the hour. A link without a signature: the session's account, by the same rules as pushes (offlineAccess, now with the certificate's hash, and for guests), one query from file to chat, and the session only when a guest may not read. Link previews take no picture from private or no-store answers. Left as before: files of direct messages stay public; a copied signed link works for anyone for its day, and a removed member keeps theirs as long. Reviews of the fixes: a send retry with an old nonce answered with the stored message from any channel, so a removed member got fresh links for their own private file (now the same channel only); a refresh dropped the message's link card; an unanswered refresh was never asked again (now the next hour). Left: a refresh restarts a playing file and does not reach an open picture viewer (a chat open for a day, within one hour).

## Private-room media without signed links

`PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS=1` (with the experiment; Helm `privateRoomMediaNoSignedLinks`): the server makes no signer, so messages carry plain links and every file request takes the session check. For operators who want no link to work outside the chat at all. Accepted: the apps, other servers' tabs and Mumble clients show private files as broken in this mode (no session with their media requests), and there is no browser cache for them.

## Push notifications: the right account, and access at send time

A security review: pushes found their user by name (the account's name, then the Mumble registration with it). A Mumble user may have registered a name before a web account took it, so that account got the user's pushes: who wrote, in which chat, also private ones. Now the hub asks PushUser (the registration with the account's certificate; none when an admin renamed it, as the login refuses it then). And a queued push went out after its user lost the chat or was banned: each note carries an Allowed check from the hub (still the account's user, not banned, may read the chat), asked when a worker sends it.

Review (a subagent): the renamed registration and a name taken by a new account while a push waits, both fixed. Left: a push stopped at send time still counts for the minute's collapse of that chat (it may hold back the next one, never leak it).

## The unread bar goes once seen

A user: after scrolling up and back down to the newest messages, the "N new messages since" bar stayed (it showed whenever the "New" line was above the view). As Discord, it goes for the rest of the visit once the reader saw the line or scrolled back down to the newest messages (`barDone` in lib/unread); the line stays until the chat closes. The chat was already read on opening. Checked in headless Chromium (Alt+click "mark unread", up a little: the bar stays; back down: gone).

## Translate messages on the device

A user asked for translation (Chinese and English in one chat); the server has no GPU. Phase 1: "Translate" in the message actions (hover bar, phone sheet) shows the translation under the message, into the app's language. On the device: the browser's Translator API (desktop Chrome, Edge) only when the pair is already there (a download would come from Google), else Bergamot, Firefox's engine (`@mkljczk/bergamot-translator`, WebAssembly in a worker, MPL-2.0; copied to /bergamot/ before builds), through English when there is no direct model. Models come only from our server: it fetches a file the first time (pinned revision of the Hugging Face mirror of Firefox's models, size and SHA-256 checked, one download for many asking at once) and keeps it; `TRANSLATION_LANGUAGES` limits the pairs (default our UI languages: at most about 500 MB). Language detection: the script (Chinese, Simplified or Traditional by marker characters; Japanese by kana), else common words of Latin-script languages; unknown or the app's language: no action. Checked in headless Chromium: a Chinese message became "I don't have to translate it into English, do I?"; the server kept the three zh-en files (54 MB). The headless shell's Translator API never answered: it has time limits now. Reviews (subagent, Astra): detection took English with "a" for Portuguese and a kaomoji for Japanese (no one-letter words now, and a script needs 30 % of the letters); a worker that did not load left "Translating…" for ever (5 minutes at most, then a new worker); translations stayed after an edit or a language change (bound to the text and the target); hidden spoilers went into the translation (left out now); the server could fetch a file twice and kept waiting for gone clients; the copy script broke on Windows.

## Better translation and message actions

Bergamot was close to useless for Chinese. Tests of small models on the hard chat lines (Qwen3.5 2B, HY-MT1.5 1.8B, Hy-MT2 1.8B, Gemma 4 E2B, all ONNX with transformers.js): Gemma 4 E2B and Hy-MT2 were best; Hy-MT2 (Tencent, Apache 2.0, 1.4 GB) with Tencent's background prompt (one sentence on the app, the messages before) and a glossary was right on nearly all lines, about 2.8 s per message on a CPU. "Better translation" (a setting, off by default): Hy-MT2 on WebGPU through transformers.js, only with 16-bit floats (checked before any download), else the smaller engines. The model comes through our server like Bergamot's (pinned ONNX build, SHA-256, kept; `TRANSLATION_LLM=0`: without); the ONNX runtime's WebAssembly is a build asset (transformers.js would take it from a CDN: its path is cleared after the import, and its blob: cache is off for the CSP). The glossary is made for each message: the app's words found in it (per language, inflected forms, whole words in Latin scripts) and the names in it (with their case, from three letters).

Detection: Latin-script lines are told by eld (n-grams, extra-small database, about 300 kB, loaded when idle; only languages with models; only a reliable answer, at least two words); before, common words missed most short French lines. eld 2.1.0, as 2.1.1 was younger than the lockfile's minimum release age.

The hover bar showed every action as a word. As Discord now: icons for React, Reply, Edit, Translate (messages in another language) and More; More and a right-click (not on links, media, inputs or selected text) open a menu with the phone sheet's list; delete asks there. Not checked in a browser with WebGPU f16 here (headless Chromium has none): the user tests on test.kallisti.chat.

## YouTube link previews through oEmbed

YouTube can return watch-page HTML without useful metadata to server requests.
Recognized video links now use its fixed HTTPS oEmbed endpoint first, without an
API key or cookies. Watch, short, Shorts, live and embed links use a validated
video ID; tracking parameters stay out of the provider request. The existing
card shows the title, channel name and proxied thumbnail. Returned embed HTML
is ignored. Other sites and general cache/retry behavior are unchanged.

Tests first failed for the missing handler, then passed with the implementation.
Local fixtures cover URL validation, bounded JSON, failed lookups, text limits,
thumbnail proxying, and private thumbnail addresses including redirects.
Independent source and Astra security reviews approved the change. The full
server race suite, vet, and existing web link-preview tests passed. No live
service or chat traffic was used for verification.

## Stop unused capture processing

Leaving voice or closing settings now pauses the capture worklet before it
collects frames or runs RNNoise. Restart reuses the node and clears partial
frames and queued echo. Final capture close sends a terminal stop, frees the
denoiser and closes the processor port. Disconnect alone did not stop work.

Two matched baseline runs and two capture-only runs in isolated LibreWolf
155.0.1-1 measured 25-second idle windows, without profiler or audio probes.
After leaving voice, mean content-process CPU fell from 3.42% to 1.84% in a
visible tab, and from 3.82% to 2.26% in a genuinely hidden tab. Fresh text idle
was unchanged at about 1.5-1.6%. Here 100% is one CPU core. These measurements
support keeping the capture change; they do not explain the reported periodic
cycle-collection bursts. Tests cover pause, restart, buffer reset and final
shutdown. Independent review found no blocking issue.

## Suspend idle voice audio

The shared voice context now suspends when no mounted client needs voice,
microphone settings or watched-stream sound. Demand survives reconnects and
context replacement. Joining voice and opening settings request sound inside
the user gesture. Stream sound keeps its clock even at zero volume or while
deafened; hiding a tab does not stop active audio. Linux startup still waits
for the capture module before starting output. A new idle context needs an
explicit suspend even if its initial state already says suspended.

With capture shutdown already applied, two further isolated LibreWolf runs,
before notification audio was created, measured fresh text content CPU at
0.12% visible / 0.32% hidden, versus about 1.5-1.6% in the earlier block.
After leaving voice it measured 0.16% visible / 0.52% hidden, then 0.12% in
a later hidden window. This is a cross-block comparison, not a separate
capture/context ABBA block; each hidden window was verified, with brief
visible transitions between them. The reduction exceeds the 0.3 percentage-point
retention threshold. A separate browser check confirmed active hidden capture
and incoming playback, native peer receipt, restart and settings capture.
Regression tests include shared owners, stream demand, pending joins, context
replacement and Linux readiness. Independent review approved the change.

## Suspend finished notification audio

Notification audio now suspends after all scheduled notes finish, including
overlapping calls. Finished nodes disconnect. A later sound awaits resume
before it reads the audio clock. In a matched context-only/full ABBA block,
after mute and unmute sounds finished, hidden-tab content CPU fell from 2.10%
to 0.46%; whole-browser CPU fell from 4.09% to 0.98%. Two runs of each build
used the same notification stimulus. This is a useful reduction, so the
notification change is kept too.

A separate hidden-tab check played both sounds to completion after restart;
the notification context returned to suspended. All 1,404 web tests, Svelte
checks and the build passed. Independent source and measurement reviews
approved keeping the changes, with the cross-block comparison limit above.
The periodic collection-heavy bursts remain unexplained. No deployment.

## 2026-11-08: Code audit follow-ups

The read-only audit of `53536341` found 23 reported defects or source findings.
They are tracked in [Code audit follow-ups](meta/issues/code-audit-followups.md).
The first fixes cover deletion-induced access changes, credential form reset,
and pending federation handoff revocation. Refactors stay inside those policy
boundaries; there is no general file-splitting or framework rewrite.

## 2026-11-08: Channel deletion: reconcile moved users and audio listeners

Channel deletion now applies the same access cleanup as a normal move. Tests prove that a moved user loses forbidden audio subscriptions, while visibility ordering, suppression, and callbacks stay correct. Full Go race tests and vet passed; 6.1-sol and Astra reviews found no blocker.

## 2026-11-08: Federation login: revoke pending handoffs with authentication

Pending federation handoffs can no longer outlive unlink, password change, or logout-all. Identity rechecks and a small shared lock keep redemption and revocation consistent. HTTP regressions and full Go race tests/vet passed; 6.1-sol and Astra reviews found no blocker.

## 2026-11-08: Desktop login: clear secrets when the server changes

Login secrets and policy now belong to a server selection. A delayed signup cannot read another server's invitation or send after the selection changes; stale challenge work cannot alter the new form. Component regressions, full web tests/type checks, and the production build passed. Reviewed by 6.1-sol and Astra.

## 2026-11-08: Message HTML: prevent styles from covering the application

Message HTML no longer retains layout-control styles. The text-only allowlist accounts for browser decoration longhands, and styles with unresolved substitutions are rejected before CSSOM serialization. Tests and headless Chrome cover safe formatting and overlay cases. Full web checks/build passed; 6.1-sol and Astra approved the final correction.

## 2026-11-08: Audio: isolate ownership of the shared DeepFilterNet node

Capture cleanup now releases only its own DeepFilterNet connection and handlers. Another server's active capture keeps its audio path and failure callbacks; the shared allocation stays cached. Fake-media regressions, full workspace tests/type checks, and the web build passed. 6.1-sol and Astra approved the fix.

## 2026-11-08: Streams: preserve both decoded audio channels

Stream playback now copies both decoded stereo channels instead of discarding the right channel. Mono playback and existing timing/gain rules stay unchanged. The right-only stereo regression, full workspace tests/type checks, and web build passed. 6.1-sol and Astra approved the fix.

## 2026-11-08: QR login: ignore completion after a server change

QR completion now checks the API and desktop server selection after its account lookup. An old server's response cannot apply an account or start a connection on the new selection. Fake-API component regressions, full workspace tests/type checks, and the web build passed. 6.1-sol and Astra approved the fix.

## 2026-11-08: Voice: refresh routing after implicit self-undeafen

A SelfMute-only unmute now invalidates receiver caches when it implicitly clears SelfDeaf. Primed-cache tests verify actual audio across normal and targeted routes, plus explicit state combinations. Full Go race tests and vet passed; 6.1-sol and Astra approved the fix.

## 2026-11-08: Channels: remove descendants before their parents

Channel removal now sorts sibling IDs before recursion, so all descendants precede their parents. Regression tests cover non-depth IDs, move/removal broadcasts, and the existing access cleanup. Full Go race tests and vet passed; 6.1-sol and Astra approved the fix.

## 2026-11-08: Uploads: reject interrupted request bodies

Upload sniffing now retains read errors and validates known body lengths before storing an upload row. Tests reproduce short and longer interruptions over local HTTP and confirm file/row/quota cleanup, successful chunked bodies, and size limits. Full Go race tests and vet passed; 6.1-sol and Astra approved the fix.

## 2026-11-08: Several servers: scope blocks and user audio preferences

Blocks, local mute, and per-user volume now belong to one server. Child views use the Client-bound policy. Ambiguous legacy settings stay stored but do not apply until users set scoped values. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Messages: preserve literal emoji references when editing

Personal emoji edit conversion now uses the same code/URL exclusions as sending. Unchanged edits preserve literal references. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Search: distinguish DM and channel targets with the same name

Search no longer loses target kind. Same-name DMs, text channels, and voice channels stay distinct; voice suggestions use in:voice:. Bare names keep the existing priority. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Build: run emoji-data generation on Windows and spaced paths

Emoji data generation now has a dedicated CLI. It cannot silently skip output because of spaces or Windows path spelling, and write failures stop the build. Subprocess tests and a fresh generated-asset build passed. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Connections: bound the initial Mumble handshake

Initial Mumble handshakes now have the same 15-second limit as reconnects. A stale deadline cannot close a newer connection. Fake-timer regressions cover sync, cancellation, replacement, and reconnects. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Desktop voice: keep TCP fallback after a UDP receive error

A fatal UDP receive error now requests TCP voice fallback even when the user is not speaking. Expected ICMP errors retain their recovery path. Offline Rust tests passed (51 unit tests, one ignored; 13 integration tests, with real-Murmur tests gated off). Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Desktop bridge: cancel blocked upstream writes when the client leaves

Native upstream writes now have a 10-second whole-operation limit. A stalled peer cannot retain the relay indefinitely, and a failed partial write ends it without another frame write. Large-frame and close-reason tests pass. Offline Rust tests passed (51 unit tests, one ignored; 13 integration tests, with real-Murmur tests gated off). Independent 6.1-sol and Astra reviews approved the fix.

## 2026-11-08: Desktop macOS: declare camera privacy permissions

Configured macOS bundle files now declare camera usage and the camera entitlement. Static tests and plist lint passed; 6.1-sol and Astra approved the configuration. Signed-app permission behavior remains unverified, so this issue stays open. No device or GUI was opened.

## 2026-11-08: Federation signup: offer a name when the remote handle is unusable locally

Federation signup now offers local-name selection when an automatic remote name is reserved or too long. It keeps the verified identity and all name checks. HTTP/component regressions and full web/Go checks passed; 6.1-sol and Astra approved the fix.

## 2026-11-08: Threads: delete large histories without exceeding SQLite limits

Large thread histories now delete with a fixed number of SQL parameters. Only attachment keys are loaded, and cleanup remains transactional. The 32,767-message and forced-rollback regressions, full Go race tests, and vet passed. 6.1-sol approved the fix.

## 2026-11-08: Sidecar: recover from outbox overflow without another event

Queued bounded sidecar recovery at overflow. The no-further-event regression, full Go race tests, and go vet passed; independent reviews approved the change.

## 2026-11-08: Messages: resolve nonce retries before new-post cooldowns

Resolved authorized message retries before new-post checks. Added production-rule regressions for cooldown, access, invalid content, and cross-limit accounting. Full Go race tests and go vet passed; independent reviews approved the change.

## 2026-11-08: Chat time-outs: disable all write paths and retain drafts

Applied one composer write policy to all entry paths without discarding unsent state. Regression tests include expiry, queued files, replies, stickers, polls, and edits. Workspace tests, typechecks, and build passed; independent reviews approved the change.

## 2026-11-08: Messages: retain edit drafts until the server accepts them

Retained edit snapshots until correlated server results, including attachment-removal choices. Added edit-only request IDs without changing manage-request behavior. Component and protocol regressions, workspace checks, and Go race tests/vet passed; independent reviews approved the change.

## 2026-11-08: Notifications: keep clicked links until the target client is ready

Moved notification ingress to App and reused the server-link path. Added fake-worker tests for pre-mount delivery, target validation, owner changes, and listener cleanup. Workspace checks passed; independent reviews approved the change. No live browser notification test was run.

## 2026-11-08: Remove Bun from devtools

The smoke and voicebot commands now run with Node and the workspace's locked `tsx` dependency. Updated setup and usage instructions. esbuild's optional platform binary works with its install script disabled.

Added local WebSocket fixture tests for synced state, Origin, bot name, spaced file paths, and outbound Opus frames. Both failed before the runner change and pass on Node 24 and 26, including with Bun absent from PATH. Frozen installation, workspace tests/typechecks, and the web build passed; independent 6.1-sol review approved the change. No live services or media devices were used. FreeBSD/Westmere execution was not tested.

## 2026-11-09: Translation: omit unused native Node backends

Omitted unused native Node translation backends with targeted package overrides. Red-to-green resolution tests and a clean browser install/build passed without them. Workspace tests/typechecks and the web build passed; independent review approved the change. No full FreeBSD support claim.

## 2026-11-09: Web tooling: remove the unused TypeScript compiler alias

Removed the redundant web compiler alias without changing other workspaces. Red-to-green tests cover alias absence and the compiler used by svelte-check. Frozen installation, workspace tests/typechecks, and build passed; independent review approved the change.

## 2026-11-09: Performance tests: honor explicit tool paths during prerequisite checks

Fixed the performance harness to honor FFMPEG and SQLITE3 overrides during prerequisite checks. Added sixteen red-to-green restricted-PATH cases, including spaced paths, invalid overrides, and disabled seeding. All devtools tests and shell syntax passed; independent review approved the change. No live service or media device ran.

## 2026-11-09: Test tooling: remove unused explicit package installations

Trimmed unused explicit test-image tools and the Rust test job package list. Both Debian image variants and all eight isolated official-client tests passed, as did YAML/shell syntax. Independent source review approved the change. Declarative removals used integration checks rather than unit TDD. The GitHub Rust job was not run; desktop packaging and required libraries are unchanged.

## 2026-11-09: Native Misskey account linking

Fediverse linking now supports native Misskey MiAuth when Mastodon app registration is unavailable. Existing OAuth providers keep their current flow. MiAuth requests only account-read permission, verifies the returned token's local account, and preserves browser/session binding and one-use callbacks for linking, login, and reauthentication.

Authentication and fediverse package tests pass, including native linking, callback tampering, expiry/replay, reauthentication, and OAuth compatibility. No live Misskey account was used.

MiAuth now attempts token self-revocation after account verification, including failed verification. Unsupported revocation does not block login. Fediverse and authentication tests pass with successful and refused revocation.
