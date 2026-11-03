# Kallisti

Voice and text chat on top of Mumble, with a Discord-like interface. Web first.
https://kallisti.chat

![Kallisti in the browser: a chat about noise suppression with a link preview, a code snippet, a reply and a mention, people in voice channels, and someone typing](docs/screenshot.png)

What it does, in short:

- **Voice**: channels you join with one click, push-to-talk or voice
  activity, noise suppression, a volume per person, music mode (stereo),
  listen-only channels, recording for moderators.
- **Video**: Go Live with your screen or camera, several streams at once,
  and streams from OBS (WHIP).
- **Chat**: text channels in categories, threads, replies, reactions, link
  previews, pictures, files and inline video, pins, search, direct
  messages, invite-only channels.
- **People**: accounts, guests, log in with your fediverse account, server
  and personal emoji, sticker packs, status and bios.
- **Moderation**: roles and channel moderators, slow mode, a word and link
  filter, limits for new accounts, a captcha, bans and time-outs.
- **Everywhere**: the browser, apps for macOS, Windows, Linux and Android
  (voice in the background), and the installed web app on phones (with push
  notifications). Mumble clients join the same channels.

All of it: [kallisti.chat/features](https://kallisti.chat/features.html).

The parts:

- `packages/protocol`: Mumble protocol (framing, protobuf codec, state, voice packets)
- `apps/web`: Svelte web client (chat, voice, video, settings)
- `apps/desktop`: Tauri desktop app around the web client
- `server`: Go server: web app, accounts, text channels, streams and a
  built-in Mumble server (Mumble clients and web clients in one place)
- `packages/devtools`: dev scripts (smoke test, voice bot)

## Deployment

Kallisti runs in one of two ways:

- **Built-in server** (recommended): Kallisti is the Mumble server. Web users,
  the desktop app and normal Mumble clients all connect to it, and every
  feature works.
- **In front of an existing Murmur**: Kallisti adds the web app, accounts and
  text channels; voice stays on your Murmur server and its Mumble users keep
  everything they have. Some features need the built-in server (see below).

Both need a domain with HTTPS: browsers allow the microphone only on secure
pages. The quickest start, with Docker and the built-in server:

```sh
umask 077
printf 'DOMAIN=chat.example.org\nSUPERUSER_PASSWORD=%s\n' "$(openssl rand -hex 16)" > deploy/.env
docker compose -f deploy/compose.yml up -d --remove-orphans
```

Open TCP 80 and 443 (the web), TCP and UDP 64738 (Mumble clients and the
desktop app) and UDP 8189 (streams from OBS). The image includes ffmpeg for
previews of videos and streams; push notifications and channel recordings
need no extra setup (see [configuration](docs/configuration.md)). Instead
of building it, Compose can pull the published image
(`ghcr.io/kallisti-chat/kallisti`, amd64 and arm64): see
[installing](docs/install-docker.md#published-images-instead-of-a-build).

- [Installing with Docker or Podman](docs/install-docker.md) (both ways,
  built here or the published images)
- [Installing without containers](docs/install-binary.md) (the program,
  systemd, Caddy)
- [Configuration](docs/configuration.md): all settings
- [Updating](docs/updating.md), with upgrade notes
- [Backups](docs/backups.md)
- [Moving from Murmur](docs/migrating-from-murmur.md): channels, users,
  groups, ACLs and bans of a Murmur server
- [Deployment guides](docs/deploy/README.md): Docker (also behind an
  existing proxy, or with Cloudflare DNS-01 and no port 80), systemd and
  Kubernetes

### What works where

| | Built-in | Murmur |
|---|:-:|:-:|
| Voice in the browser and the desktop app, avatars, private messages | ✓ | ✓ |
| Text channels with history, uploads, mentions, replies, search | ✓ | ✓ |
| Edit and delete your own messages; remembered read state | ✓ | ✓ |
| Ban list and registered users in the web app (with Mumble rights) | ✓ | ✓ |
| Lobby: users join voice channels on purpose | ✓ | – |
| Manage text channels and topics in the app (admins, moderators) | ✓ | – (env) |
| Moderator role; moderators delete others' messages | ✓ | – |
| Stored voice channel chats | ✓ | – |
| Text channels for Mumble clients (mirrors) | ✓ | – |
| Guest accounts; reactions for guests | ✓ | – |
| Log in as SuperUser with the "Log in" tab | ✓ | – |
| Threads, personal emoji, log in with the fediverse | ✓ | ✓ |
| Music mode (128 kbps stereo; Murmur's `bandwidth` must allow it) | ✓ | ✓ |
| Go Live (screen, camera), streams from OBS | ✓ | – |
| Text channel categories, invite-only channels | ✓ | – |
| Server emoji (adding them), stickers | ✓ | – |
| Channel recording | ✓ | – |
| Slow mode, word and link filter, new-account limits, captcha | ✓ | – |
| Read-only guests, deleting many messages at once | ✓ | – |
| Listen-only channels, channel moderators | ✓ | – |
| Push notifications; the same account on several devices | ✓ | – |

### For people with Mumble clients

With the built-in server, Kallisti is a full Mumble server: people keep
their client (Mumble 1.3 or newer) and everything they know works. That is
voice channels, temporary and linked channels, whispering and shouting,
listening to other channels, positional audio, comments and avatars,
registering with their certificate, private messages, rights (ACLs and
groups), bans and, for admins, the list of registered users.

On top of that, they get these things, without installing anything:

- **Friends without Mumble in the same channels.** Anyone can join from a
  browser by opening the site (or with the desktop app), and talks with them like any
  Mumble user, with their avatar.
- **Text channels.** The web app's text channels appear as channels under
  "Text channels". What they write there reaches web users, and web users'
  messages arrive live.
- **Chat that stays.** What is written in text and voice channels is kept.
  When they enter a channel, the server sends what was said there as one
  message: registered users get what they missed since their last visit
  (also days later), guests the newest 20. People who come later read it in
  the web app too, and it can be searched.
- **Pictures and files.** Pictures that registered users send are kept with
  the message; files from web users arrive as links, and replies with a
  short quote in front.
- **A lobby.** The top channel is for meeting; to talk, people join a voice
  channel on purpose (no surprise microphone in the root channel).
- **Protected names.** Names of Kallisti accounts are theirs only: nobody
  can join with them as a Mumble guest.
- **Moderators.** Admins make moderators with one click in the web app, and
  moderators can kick, ban, move and change channels from their Mumble
  client too.
- **A real certificate.** With `MUMBLE_CERT_FILE`, the Mumble port uses the
  site's certificate, so clients connect without a trust question.
- **An admin page in the browser.** SuperUser logs in on the web and manages
  bans, registered users and channels there.
- **Streams they can hear.** When someone goes live in their channel, the
  stream's sound reaches them as a separate user (the picture is for the
  web app and the desktop app).
- **Channels as they know them.** Text channel categories and invite-only
  channels are Mumble channels with ACLs; listen-only channels and channel
  moderators are plain ACLs and groups they can see.
- **The same rules.** Slow mode, the word and link filter and the limits for
  new accounts also apply to what they write; when a channel is recorded,
  they are told when they enter.

What Mumble clients do not see: reactions, threads, emoji, stickers (they see their name), later edits and
deletions, typing, link previews and formatting (they get the plain text). Not there
compared with Murmur: plugin data between clients (for example game
positions shared by Mumble plugins), menu entries that server scripts add
through Murmur's Ice interface, and clients older than 1.3 (no Opus).

**In front of an existing Murmur**, nothing changes for Mumble users:
Murmur stays their server. Web users join them as normal Mumble users
(accounts as registered users); text channels and their history are for
web users only.

### Bots

Bots are plain Mumble clients, so any Mumble bot works. For example,
[kallisti-jukebox](https://github.com/kallisti-chat/kallisti-jukebox) plays
music from Jellyfin in a voice channel, controlled with chat commands.

## Website

`site/` is the website at [kallisti.chat](https://kallisti.chat): plain HTML,
CSS and a little JavaScript (no build step), published to GitHub Pages by
`.github/workflows/site.yml` on every change.

## Desktop app

`apps/desktop` is a Tauri app around the same web client. With a Kallisti
server it works like the web app (accounts, text channels, everything). Any
other address goes through a small bridge in the app (Rust), straight to a
Mumble server, with voice and Mumble's chat. Needs Rust. The iOS app has
no bridge (Kallisti servers only): its encryption is then Apple's alone,
and its plist says so (no export compliance question per upload).

```sh
pnpm --filter @kallisti/desktop dev                  # window with the Vite dev server
pnpm --filter @kallisti/desktop build --bundles app  # Kallisti.app
```

In `dev` the window's page comes from the Vite dev server
(`http://localhost:5173`), which Kallisti servers do not allow: there they
look like plain Mumble servers. Use a build, or a local server with
`http://localhost:5173` in `ALLOWED_ORIGINS`.

GitHub builds the app for macOS, Windows and Linux on pushes to `main`
(downloads on the Actions run) and makes a draft release for tags `v*`
(`.github/workflows/desktop.yml`). The macOS build is signed and notarized
(repository secrets); Windows and Linux are not signed yet. The Android APK
(arm64) comes from `.github/workflows/android.yml` the same way, signed with
the release key from repository secrets. The container images go to the
GitHub Container Registry from `.github/workflows/image.yml` (amd64 and
arm64): `edge` on pushes to `main`; for tags `v*` the version (`0.10.1`,
`0.10`) and `latest`. New packages of the organisation are private: make
each public once (Packages, the package, Package settings, Change
visibility), or others cannot pull them.

## Development

Needs Node with pnpm, Go and bun.

```sh
pnpm install
pnpm test        # TypeScript and Go tests

# Server: web clients on port 8080, Mumble clients on 64738
cd server
ALLOWED_ORIGINS='*' go run ./cmd/server

# Web client on http://localhost:5173 (server defaults to port 8080)
pnpm --filter @kallisti/web dev
```

Settings: see [configuration](docs/configuration.md).

The official Mumble client in podman, for the server tests:
`KALLISTI_OFFICIAL_CLIENT=1 go test ./internal/mumbled` (see
`packages/devtools/mumble-client`).

Dev scripts (with a running bridge):

```sh
pnpm --filter @kallisti/devtools smoke ws://127.0.0.1:8080
pnpm --filter @kallisti/devtools voicebot tone.opus ws://127.0.0.1:8080
```

## License

Public domain: [the Unlicense](LICENSE). The web app includes RNNoise and
DeepFilterNet3 (for noise suppression) under their own licenses: see
[third-party-notices.txt](apps/web/public/third-party-notices.txt), which the
built app also serves.
