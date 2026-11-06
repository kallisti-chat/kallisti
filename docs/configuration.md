# Configuration

Kallisti reads its settings from environment variables. Defaults are in
brackets.

- With the systemd service: `/etc/kallisti.env`.
- With compose: only the variables in the `environment:` section of the
  compose file reach Kallisti (some of them take their value from
  `deploy/.env`). A setting that is only in `deploy/.env` does nothing: add
  it to `environment:` too.
- In `deploy/.env`, compose reads `$` as the start of a variable: put
  values with `$` in single quotes (`SUPERUSER_PASSWORD='pa$$word'`).

## Web and general

- `ALLOWED_ORIGINS` (required): the address of the web app, for example
  `https://chat.example.org`; comma-separated for several, `*` only for
  development.
- `LISTEN_PORT` (8080): the web app and the web clients' connections.
- `WEB_DIR` (none): the built web app (`apps/web/dist`). Without it the
  server only answers web clients' connections (development with Vite).
- `TRUST_FORWARDED_FOR=1`: use the last `X-Forwarded-For` address (the one
  your reverse proxy adds) as the client's IP. Only with exactly one proxy
  in front that sets it: clients can fake the header. Caddy replaces the
  header and nginx with `$proxy_add_x_forwarded_for` adds to it; both work.
  With two proxies (for example Cloudflare and nginx), all users would get
  the first proxy's address.
  It also trusts `X-Forwarded-Proto` for HTTPS: secure login cookies (with
  the `__Host-` prefix) need the proxy to set it (Caddy does; nginx:
  `proxy_set_header X-Forwarded-Proto $scheme;`).
  The web listener itself serves plain HTTP. If `ALLOWED_ORIGINS` includes
  an HTTPS address without `TRUST_FORWARDED_FOR=1`, startup logs a warning:
  login cookies will lack `Secure` and the `__Host-` prefix. Startup still
  succeeds. Enable trust only when the proxy sets these headers and clients
  cannot reach the app port directly; otherwise clients can forge both
  their IP and the HTTPS indication. The supplied Compose and Kubernetes
  configurations already enable trust; check network access to the app port
  separately, including access from other pods in Kubernetes.
- `MAX_CONNECTIONS` (100) and `MAX_CONNECTIONS_PER_IP` (10): web connections. Per-address limits
  (also login attempts) count an IPv6 /64 as one address: devices on one
  network that share a /64 share the limit.
- `DB_PATH` (`kallisti.db`): the SQLite database.
- `TEXT_CHANNELS` (`general`, comma-separated): with the built-in server
  only the first start uses it, then admins manage text channels in the app;
  in front of a Murmur it is read at each start.
- `UPLOAD_DIR` (`uploads`), `UPLOAD_MAX_MB` (10 per file),
  `UPLOAD_QUOTA_MB` (100 per user per day).
- `EXPERIMENTAL_PRIVATE_ROOM_MEDIA=1` (off by default): files of private
  rooms only for who may read the room (built-in server only). See
  [private-room media](#private-room-media-experiment).
  `PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS=1` with it: no signed links (see there).
- `BACKUP_DIR` (none: no backups) and `BACKUP_KEEP` (7): see
  [backups](backups.md).
- `LINK_PREVIEWS=0`: the server fetches no pages for link previews. By
  default it fetches the first link of a message, only from public
  addresses, so linked sites see the server's address.
- `FEDI_LOGIN=0`: no "Log in with the fediverse". By default users can
  log in, create accounts and link accounts with a fediverse account
  (Mastodon API: Mastodon, Pleroma/Akkoma, GoToSocial, ...). It needs the
  server's public address: the first `ALLOWED_ORIGINS` entry (not `*`),
  where instances send the browser back to (`/api/fedi/callback`). The
  server contacts the instance users name, only on public addresses and
  HTTPS. A linked fediverse account logs in to its Kallisti account, so
  the admins of its instance could too: users should link accounts only
  on instances they trust.
- `FEDI_INSECURE_DEV_INSTANCES=1`: development and tests only. Fediverse
  instances may also be local addresses and plain HTTP (for a fake test
  instance, see `server/cmd/fakefedi`). Never on a public server: users
  could make the server send requests into its local network.
- `FFMPEG_PATH` (none: `ffmpeg` on `PATH`) and `MEDIA_PREVIEWS=0`: with
  ffmpeg, videos get a thumbnail (a frame at 1 s, at most 480 px) and large
  pictures (over 1600 px on a side, or over 1 MB) a smaller copy for the
  chat (1000 px); the original opens in the lightbox. WebP if the ffmpeg
  build has libwebp, else JPEG (then PNG and WebP pictures get none, to keep
  their transparency). At most 2 ffmpeg processes run at once, at low
  priority, for at most 20 s each, on the stored file only. The log says at
  start whether previews are on. Without ffmpeg, or with
  `MEDIA_PREVIEWS=0`, there are none. The Docker image has ffmpeg (about
  110 MB more).

## The built-in Mumble server

Used unless `MUMBLE_HOST` is set.

- `MUMBLE_LISTEN` (`:64738`, TCP and UDP): Mumble clients and the desktop
  app.
- `MUMBLE_PUBLIC_PORT` (the port of `MUMBLE_LISTEN`): the Mumble port as
  clients reach it, if Docker or a firewall maps it to another one. The
  desktop and Android apps send voice to it over UDP (better on bad
  networks); without UDP they keep voice in the WebSocket. `0`: never UDP.
- `SERVER_NAME` (`Kallisti`), `MUMBLE_WELCOME` (the welcome text).
- `MUMBLE_PASSWORD`: a server password for guests. With it, saved avatars
  are not served to everyone (the server is private).
- `MUMBLE_MAX_USERS` (100), `MUMBLE_MAX_PER_IP` (no limit).
- `MUMBLED_SUPERUSER_PASSWORD`: sets SuperUser's password at the start
  (unset: the password from an earlier start stays).
- `CERT_DIR` (the folder of `DB_PATH`): the self-signed server certificate,
  made on the first start.
- `MUMBLE_CERT_FILE` and `MUMBLE_KEY_FILE`: PEM files of a certificate for
  the server's name (for example Let's Encrypt), so Mumble clients do not ask
  to trust a self-signed one. Reloaded when they change; until they exist the
  self-signed certificate is used. They must be readable by the server's
  user.
- Push notifications (on by default): users with a password can turn them
  on in Settings. A mention, a reply, `@everyone` (in a channel they may
  read) or a direct message then reaches their browsers and installed apps,
  but only while they have no open connection (the open app notifies
  them) and are not in "do not disturb". `@here` sends none. The push says
  who and where, never the message. At most one per user and chat per
  minute (later pings in that minute are dropped). A channel set to
  "Nothing" on a device, and users blocked there, send none to that
  device; mutes do not stop mentions, as in the app. A subscription ends
  with the login session that made it (log out, password change, expiry).
  The server sends only to the browsers' push services (Google, Mozilla,
  Apple, Microsoft), so it needs outgoing HTTPS, and browsers allow push
  only over HTTPS (or on `localhost`).
  - `PUSH=0`: no push notifications.
  - `VAPID_PUBLIC_KEY` and `VAPID_PRIVATE_KEY` (made on the first start and
    kept in the database): the server's push keys, base64url. With changed
    keys, browsers subscribe again when the app next opens there.
  - `PUSH_SUBJECT` (the first `ALLOWED_ORIGINS` address if it is HTTPS,
    else `https://kallisti.chat`): a `mailto:` or `https:` contact that
    push services see.
- `RECORDING_DIR` (`recordings` next to `DB_PATH`): channel recordings
  (admins and moderators record a voice channel; one Ogg Opus file per
  speaker). Back it up or not: finished recordings are removed after
  `RECORDING_KEEP_DAYS` (7).
- `RECORDING_MAX_MINUTES` (240) and `RECORDING_MAX_MB` (1024): one
  recording stops by itself at either limit.

### Invite-only registration

Admins turn it on in Server settings → Registration (off by default, also
after an upgrade). Then a new account needs an account invitation link from
an admin, and new guests (web and Mumble) are refused. Existing accounts,
their sessions and registered Mumble users keep their access; guest
accounts from before can still get a password. Channel invitations do not
admit new accounts. It needs the built-in Mumble server.

Bots that connect over Mumble (Jukebox, Kallisti TV) count as guests unless
their certificate is registered: register them before the switch goes on
(connected, a SuperUser or admin registers them in a Mumble client, or the
bot registers itself), else they are refused.

### Streams from OBS (WHIP)

Users stream from OBS 30 or newer (or another WHIP encoder) into their voice
channel; it shows like a screen share. Each user makes a stream key in a
voice channel: voice bar → "Go live" → "Stream from OBS…", which also
shows the server address
(`https://<your domain>/api/whip`) and the OBS settings. Off unless
`WHIP_UDP_PORT` is set (`deploy/compose.yml` sets it).

- `WHIP_UDP_PORT` (off): one UDP port for all streams (8189 in the compose
  file). Open it in the firewall. No TURN or STUN server is needed.
- `WHIP_PUBLIC_IP`: the server's public IPv4 address (or a name, resolved at
  the start), sent to OBS as the address to reach. Needed where the server
  does not see it, as in Docker (the compose file uses `DOMAIN`). Behind
  Cloudflare's proxy, set the IP address itself. Unset: the addresses of the
  server's interfaces.
- Only IPv4, only UDP (OBS has no ICE over TCP).
- A stream needs a registered account (no guest accounts), connected to
  voice in a channel where it may speak. A new password or "log out
  everywhere" removes the key. A stream ends when OBS stops, the
  connection fails, or the user leaves the channel or disconnects.
- OBS must send H.264 (packetization-mode 1) and Opus. The server forwards
  the video as it comes: viewers need a browser that decodes H.264, and the
  stream should have no B-frames (x264: `bframes=0` in "x264 Options";
  NVENC, QuickSync, AMF: B-Frames 0), a keyframe every 1 or 2 seconds, and
  at most 4096 pixels per side (larger streams are refused). Pictures above
  8 Mbit/s are dropped; at most 20 streams from OBS at once.

## In front of an existing Murmur

- `MUMBLE_HOST`: the Murmur server (this turns the built-in server off).
- `MUMBLE_PORT` (64738).
- `MUMBLE_INSECURE_TLS=1`: accept Murmur's self-signed certificate. The
  server defaults to checking the certificate; `deploy/compose.murmur.yml`
  keeps its earlier default of 1 for common self-signed Murmur setups.
  Set it to 0 in `deploy/.env` when Murmur has a trusted certificate.

## Development

- `VITE_PROXY_URL` (web client, build time): the WebSocket address of another
  server (`wss://host/ws`); the API address comes from it.

## Private-room media experiment

Without it, anyone with a file's link can load the file. With
`EXPERIMENTAL_PRIVATE_ROOM_MEDIA=1`, a room's files (originals, previews,
downloads) follow the room's current access, as its chat does. Threads follow
their channel. Files of public rooms stay readable by everyone; files in
direct messages keep their old rule (anyone with the link).

Enable it in the server environment and restart: in Compose,
`EXPERIMENTAL_PRIVATE_ROOM_MEDIA: "1"` in the `kallisti` service's
`environment`; in the Helm chart, `experimentalPrivateRoomMedia: true`. It
needs the built-in Mumble server: with `MUMBLE_HOST` the server does not start.
Turn it off and restart, and file links are public again.

How it works:

- The server gives file links only to a chat's readers, in its messages.
  These links are signed and work for a day (24 to 25 hours), with no session,
  as Discord's. So the web client, the desktop and Android apps, other servers'
  tabs and Mumble clients (links in their chat) all load the files. A client
  that stays open asks for fresh links before they expire.
- A link copied out of the chat works for anyone until it expires. A member
  removed from the room also keeps the links they have until then.
- A link without a signature (an old one, or a copied link without its query)
  works for a browser logged in to this server whose account may read the
  room. Otherwise the answer is 404, the same as for a file that does not
  exist.
- Files not yet sent have no room: 404 (the composer shows its own previews).

Without signed links (`PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS=1`, Helm
`privateRoomMediaNoSignedLinks: true`): messages carry plain links, and every
file request needs a browser logged in to this server whose account may read
the room. No link works outside the chat, not even for a day. The price: the
desktop and Android apps, other servers' tabs and Mumble clients show the files
of private rooms as broken (their pictures and videos send no session), and
each request checks the session and the room again (no browser cache).

Caches: a signed link's file may stay in the reader's browser cache
(`private`); other answers are `private, no-store`. The server's link previews
show no picture from such answers. Files that were served before the setting
was on may still be in shared caches (one year, `public`): enabling it cannot
take those back.
