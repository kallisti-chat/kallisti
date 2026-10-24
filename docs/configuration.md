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
- `MAX_CONNECTIONS` (100) and `MAX_CONNECTIONS_PER_IP` (10): web connections. Per-address limits
  (also login attempts) count an IPv6 /64 as one address: devices on one
  network that share a /64 share the limit.
- `DB_PATH` (`kallisti.db`): the SQLite database.
- `TEXT_CHANNELS` (`general`, comma-separated): with the built-in server
  only the first start uses it, then admins manage text channels in the app;
  in front of a Murmur it is read at each start.
- `UPLOAD_DIR` (`uploads`), `UPLOAD_MAX_MB` (10 per file),
  `UPLOAD_QUOTA_MB` (100 per user per day).
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
- `REGISTRATION=0`: no new accounts. The web app has no "Create account"
  tab, and the server refuses new accounts by any way: the form, a
  fediverse login without an account here, and a guest account kept with
  a password or a fediverse link. Existing accounts log in as before, and
  guests still join (with `MUMBLE_PASSWORD`, they need it). Mumble clients
  are not affected: they can still register themselves on the built-in
  server (a server password keeps strangers out).
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
  stream should have no B-frames, a keyframe every 1 or 2 seconds, and
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
