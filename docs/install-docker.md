# Installing with Docker or Podman

The compose files in `deploy/` run Kallisti and Caddy, which gets the HTTPS
certificate by itself. Browsers allow the microphone only on secure pages,
so Kallisti needs a domain with HTTPS. Run the commands below from the
repository root. Existing installations should follow
[the upgrade instructions](updating.md#docker-or-podman) to keep their data.

## Built-in Mumble server (recommended)

`deploy/compose.yml` runs Kallisti (web app, voice, text channels, uploads)
and Caddy:

```sh
umask 077
printf 'DOMAIN=chat.example.org\nSUPERUSER_PASSWORD=%s\n' "$(openssl rand -hex 16)" > deploy/.env
docker compose -f deploy/compose.yml up -d --remove-orphans
```

- Open `https://chat.example.org`. Mumble clients and the desktop app
  connect to the same host on port 64738 (TCP and UDP): open it in your
  firewall.
- Streams from OBS (WHIP) use UDP port 8189: open it in the firewall too.
  OBS reaches it at the address of `DOMAIN`; behind Cloudflare's proxy set
  `WHIP_PUBLIC_IP` to the server's IPv4 address in `deploy/.env` (see
  [configuration](configuration.md#streams-from-obs-whip)).
- `SuperUser` is the admin: log in with the "Log in" tab (or a Mumble
  client) and `SUPERUSER_PASSWORD`. Admins make moderators in a user's menu.
- Your own password in `deploy/.env`: put it in single quotes
  (`SUPERUSER_PASSWORD='pa$$word'`). Without them, compose reads `$` as the
  start of a variable and changes the password.
- Caddy gets the web certificate automatically. The `cert-sync` service
  copies a validated certificate and key to Kallisti's volume on startup
  and checks for renewals every minute. Until Caddy has a certificate,
  native Mumble clients see Kallisti's self-signed one. Check
  `docker compose -f deploy/compose.yml logs cert-sync` if that
  warning persists after HTTPS is working. The copier needs read-only
  access to Caddy's private key and write access to Kallisti's data; keep
  Docker access restricted to trusted administrators.
- `docker compose -f deploy/compose.yml ps` shows the copier's health with
  recent cert-sync images. Waiting for Caddy's first certificate is normal;
  rejected certificates or a stalled copier are unhealthy. See the
  [healthcheck details](deploy/docker.md#operations) for diagnosis.
- Behind a reverse proxy that already runs in Docker, use
  `deploy/compose.behind-proxy.yml`: set `PROXY_NETWORK` to the proxy's
  Docker network and point the proxy at `kallisti:8080`. This file has its
  own project name (`kallisti`), so it gets a new, empty volume
  (`kallisti_kallisti-data`): when you change from `compose.yml`, copy the
  data over first. The external proxy must set the client's forwarded IP
  itself. Its certificate is not available to `cert-sync`: Mumble uses the
  self-signed certificate unless you supply a certificate and key under
  `/data` and set `MUMBLE_CERT_FILE` and `MUMBLE_KEY_FILE` in the Compose
  environment.
- Coming from Murmur: `--remove-orphans` stops an old Murmur container (it
  would keep port 64738). Murmur's registrations, channels and ACLs are not
  taken over.

### Cloudflare DNS-01 without public HTTP port 80

For the built-in Mumble server when public port 80 cannot be opened, add
`deploy/compose.dns01.yml` on top of `deploy/compose.yml` with Docker Compose
2.24.4 or later (`!override` is required). It builds Caddy 2.11.7 with the
Cloudflare DNS plugin, obtains the same certificate through
DNS-01, and keeps HTTPS 443, native Mumble TCP/UDP 64738, WHIP UDP 8189,
certificate sync, and the `deploy_` volumes. It does not publish HTTP port 80;
HTTP URLs will not redirect to HTTPS. Run only one Compose variant at a time.

Create a Cloudflare API token limited to the relevant zone with `Zone.Zone:Read`
and `Zone.DNS:Edit` permissions. Store it as a nonempty, single-line file **outside
this repository**, readable only by the deployment operator (for example,
mode 0600). Add `CLOUDFLARE_API_TOKEN_FILE=/absolute/path/to/token` to
`deploy/.env` alongside `DOMAIN` and your other settings; do not put the token
itself in `.env` or in a command line. The path must exist on the machine
running Compose. The token is mounted read-only, loaded into Caddy's process
environment at startup, and is still accessible to Docker administrators;
protect Docker access and the token file. Caddy fails to start if the file is
missing or empty.

```sh
docker compose -f deploy/compose.yml -f deploy/compose.dns01.yml config --quiet
docker compose -f deploy/compose.yml -f deploy/compose.dns01.yml up -d --build --remove-orphans
```

Use a DNS-only Cloudflare record for `DOMAIN` if Mumble clients connect to
that name: Cloudflare's web proxy does not forward native Mumble or WHIP
traffic. Point the name to this host, open HTTPS 443 and the native ports,
and verify both HTTPS and a Mumble connection. Watch `caddy` and `cert-sync`
logs if issuance or native TLS fails. Before switching back to
`deploy/compose.yml`, open port 80 for its usual HTTP-based certificate
issuance; keep the `deploy_` data volumes and your `.env`.

## Published images instead of a build

Every release is also an image on the GitHub Container Registry, for
amd64 and arm64: `ghcr.io/kallisti-chat/kallisti` (and
`ghcr.io/kallisti-chat/kallisti-cert-sync`). With
`deploy/compose.published.yml` on top, Compose pulls them instead of
building from the source; updates are then `pull` and `up -d`, without a
new checkout (only the `deploy/` files and `deploy/.env` are needed):

```sh
docker compose -f deploy/compose.yml -f deploy/compose.published.yml up -d --remove-orphans
```

- `KALLISTI_TAG` in `deploy/.env` picks the version: `latest` (the newest
  release, the default), a version such as `0.10.1`, or `edge` (the main
  branch, untested between releases).
- The same file goes on top of `compose.dns01.yml` or
  `compose.behind-proxy.yml`.

## In front of an existing Murmur

`deploy/compose.murmur.yml` runs Kallisti and Caddy, without a Mumble port:

```sh
umask 077
printf 'DOMAIN=chat.example.org\nMUMBLE_HOST=voice.example.org\n' > deploy/.env
docker compose -f deploy/compose.murmur.yml up -d
```

- The Murmur variant keeps the previous default of accepting self-signed
  certificates. Set `MUMBLE_INSECURE_TLS=0` in `deploy/.env` if Murmur has
  a trusted certificate and you want Kallisti to verify it.
- Each web user gets their own Mumble session on your Murmur, made by the
  Kallisti server. Murmur therefore sees all web users at the Kallisti
  server's address: set `autobanAttempts=0` in `murmur.ini`, or Murmur may
  ban Kallisti after a few reconnects.
- Accounts register themselves on Murmur with their own certificate (Murmur
  allows that by default). The admin stays Murmur's SuperUser: join with the
  "Guest" tab, the name `SuperUser` and the SuperUser password as the server
  password.
- `TEXT_CHANNELS` is the list of text channels; it is read at each start.

## Data

Everything lives in the `kallisti-data` volume (`/data` in the container):
the database, uploads, database backups (`/data/backups`) and, with the
built-in server, its certificate. See [backups](backups.md).

Kallisti runs as user and group 1000. To use a host folder for `/data`
(for example on a NAS), mount it in place of the volume and set its
owner's ids in `deploy/.env`:

```sh
PUID=1001
PGID=100
```

The folder must be writable by that user. A named volume belongs to 1000;
after a change of the ids, `chown -R` its files to the new ones.

The image includes ffmpeg for video thumbnails and picture previews (about
110 MB of the image; `MEDIA_PREVIEWS=0` turns them off). It works with the
read-only container: ffmpeg writes to the `/tmp` tmpfs, the previews go
next to the uploads in `/data/uploads`.

Bans and limits use the client's IP. Some rootless podman networks
(slirp4netns with rootlesskit) rewrite the source address of all clients,
web and native, so they all share one IP; Docker, podman with a root
network, or pasta keep it.

On Fedora and other SELinux systems, add `,z` to the Caddyfile mount
(`./Caddyfile:/etc/caddy/Caddyfile:ro,z`), or Caddy cannot read it. If you
raise `UPLOAD_MAX_MB` above 10, also raise the 11 MB request-body limit in
`deploy/Caddyfile`. For ports, volumes, backups and checks, see the
[Docker deployment guide](deploy/docker.md).

## Updates

See [updating](updating.md#docker-or-podman).
