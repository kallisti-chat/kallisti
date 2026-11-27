# Installing without containers

Kallisti is one Go program plus the built web app (a folder of files). It
needs no database server: the data is one SQLite file. You need a reverse
proxy for HTTPS in front of it (Caddy below; nginx works too).

## Build

You need Node 24 with pnpm 11 and Go 1.27.4 or newer (the `Dockerfile`
shows the exact versions it builds with). On the build machine:

```sh
git clone https://github.com/kallisti-chat/kallisti.git
cd kallisti
pnpm install --frozen-lockfile --filter @kallisti/web...
pnpm --filter @kallisti/web build                       # -> apps/web/dist
cd server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../kallisti ./cmd/server
cd ..
```

The program has no other dependencies (no cgo), so you can build it on
another machine: add `GOOS=linux GOARCH=amd64` (or `arm64`) for the server.
Optional: install ffmpeg on the server (`apt install ffmpeg`) for video
thumbnails and picture previews (see `FFMPEG_PATH` in
[configuration](configuration.md)).

## Install

On the server, as root, from the repository root (or a copy of `kallisti`,
`apps/web/dist` and `deploy/`):

```sh
useradd --system --home /var/lib/kallisti --shell /usr/sbin/nologin kallisti
mkdir -p /opt/kallisti
cp kallisti /opt/kallisti/kallisti
cp -r apps/web/dist /opt/kallisti/web
install -o root -g root -m 0644 deploy/kallisti.service /etc/systemd/system/kallisti.service
```

Settings go into `/etc/kallisti.env` (readable by root only: it holds the
SuperUser password). A minimal file for the built-in Mumble server:

```sh
ALLOWED_ORIGINS=https://chat.example.org
TRUST_FORWARDED_FOR=1
WEB_DIR=/opt/kallisti/web
DB_PATH=/var/lib/kallisti/kallisti.db
UPLOAD_DIR=/var/lib/kallisti/uploads
BACKUP_DIR=/var/lib/kallisti/backups
MUMBLE_CERT_FILE=/var/lib/kallisti/mumble-tls/current/mumble.crt
MUMBLE_KEY_FILE=/var/lib/kallisti/mumble-tls/current/mumble.key
LISTEN_PORT=8080
MUMBLED_SUPERUSER_PASSWORD=choose-a-long-password
```

All settings: [configuration](configuration.md). For an existing Murmur, add
`MUMBLE_HOST`, and `MUMBLE_INSECURE_TLS=1` if Murmur has a self-signed
certificate (most do); see [the Docker page](install-docker.md#in-front-of-an-existing-murmur)
for what to set on Murmur.

```sh
chmod 600 /etc/kallisti.env
systemctl daemon-reload
systemctl enable --now kallisti
journalctl -u kallisti -f      # "server running" when it is up
```

The service file runs Kallisti as the `kallisti` user with its data in
`/var/lib/kallisti` (systemd makes the folder) and keeps it from changing
the rest of the system: data paths elsewhere need `ReadWritePaths=` in the
service file.

## HTTPS with Caddy

Install Caddy with its systemd service, then copy the supplied configuration
and replace `chat.example.org` with your domain before starting it:

```sh
install -o root -g root -m 0644 deploy/systemd/Caddyfile /etc/caddy/Caddyfile
# Edit /etc/caddy/Caddyfile to use your domain.
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl enable --now caddy
systemctl reload caddy # if it was already running
```

Caddy gets the certificate by itself. Its configuration adds HTTPS security
headers and limits uploads to 11 MB (for the default 10 MB file limit). If
you increase `UPLOAD_MAX_MB`, also increase the limit in the Caddyfile.
WebSockets (the voice and chat connection) pass through without extra
settings. Kallisti listens on all addresses: block port 8080 in the
firewall, so only the proxy reaches it.

Kallisti needs its own (sub)domain: it cannot run under a path such as
`example.org/chat`.

### nginx (or another proxy)

```
server {
    listen 443 ssl;
    server_name chat.example.org;
    # ssl_certificate ...;
    client_max_body_size 11m;        # uploads (UPLOAD_MAX_MB, 10) and a little more
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;      # WebSockets
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

The default read timeout (60 s) is enough: the server pings each connection
every 10 seconds. Use one proxy only (see `TRUST_FORWARDED_FOR` in the
[configuration](configuration.md)). With SELinux, a confined nginx needs
`setsebool -P httpd_can_network_connect 1` to reach port 8080.

## Ports

- 80 and 443 (TCP): the proxy (80 for the certificate and the redirect).
- 64738 (TCP and UDP): Mumble clients and the desktop app, with the
  built-in server. Behind Cloudflare, the name for this port must be "DNS
  only": Cloudflare does not pass it.
- 8189 (UDP), if `WHIP_UDP_PORT=8189` is set: streams from OBS (see
  [configuration](configuration.md#streams-from-obs-whip)).
- 8080: only for the proxy.

## The Mumble port

With the built-in server, Mumble clients and the desktop app connect to port
64738 (TCP and UDP) directly, not through the proxy: open it in the
firewall.

Until Caddy gets a certificate, native clients see Kallisti's saved
self-signed certificate. Install the supplied timer to copy valid renewals
for the Mumble port without giving Kallisti access to Caddy's private store:

```sh
install -o root -g root -m 0755 deploy/docker/sync-certificate.sh /opt/kallisti/sync-certificate
install -o root -g root -m 0644 deploy/systemd/kallisti-certificate.service /etc/systemd/system/
install -o root -g root -m 0644 deploy/systemd/kallisti-certificate.timer /etc/systemd/system/
printf 'DOMAIN=chat.example.org\n' > /etc/kallisti-cert.env
chmod 600 /etc/kallisti-cert.env
systemctl daemon-reload
systemctl enable --now kallisti-certificate.timer
systemctl start kallisti-certificate.service # after Caddy has HTTPS
journalctl -u kallisti-certificate.service -n 30
```

The root-only service checks every five minutes and only writes into
`/var/lib/kallisti`. It fails if Caddy has not issued a matching certificate
yet; the next timer run retries. Caddy's default systemd data path is
`/var/lib/caddy/.local/share/caddy`; if your installation uses another path,
set `CADDY_DATA_DIR` in `/etc/kallisti-cert.env`. Keep the timer's `DOMAIN`
matched to the name in `/etc/caddy/Caddyfile`. See the
[systemd deployment guide](deploy/systemd.md) for checks and rollback.

## Updates and backups

See [updating](updating.md#without-containers) and [backups](backups.md).
