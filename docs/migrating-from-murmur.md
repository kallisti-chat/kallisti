# Moving from Murmur

`kallisti import-murmur` takes a Murmur (mumble-server) database into a new
Kallisti server: the channels (names, descriptions, order, links, ACL
inheritance), ACLs, groups with their members, registered users (name,
certificate, comment, avatar, last channel), bans and the server name.
Murmur 1.4 and 1.5 databases work (schema version 9).

Users keep logging in from their Mumble client with their certificate, as
before. Their name stays theirs: on the web nobody else can take it. They
cannot log in on the web with it yet (a Mumble registration has no web
password); for the web app they make an account with another name.

## Before you start

- Use a new Kallisti server: the import refuses a server with registered
  users or channels of its own. A server that was started once is fine
  (its automatic "General" channel goes, the text channels stay).
- Stop Kallisti, and copy Murmur's database (`murmur.sqlite` or
  `mumble-server.sqlite`) while Murmur is stopped.
- See what will happen first, with `-dry-run`: it writes nothing.

The import makes a database backup first (in `BACKUP_DIR`, or `backups`
next to the database).

## Several virtual servers

A Murmur database can hold several virtual servers. Kallisti has one
server, so by default each virtual server becomes a channel under root,
with its channels below. Its root ACL and groups go to that channel, so
its admins manage that part; Kallisti's admins manage everything. Rights
that work only in root (Kick, Ban, Register) do nothing there: give them on
Kallisti's root. Users are numbered anew; a certificate on several servers
is one user (with the groups of all), and a name that a user of an earlier
server has gets a suffix ("name-2"). The import lists the changes.

With `-server ID` only that virtual server is imported, as the whole
Kallisti server: its root's ACL and groups become Kallisti's (its admins
are Kallisti's admins), and its name becomes the server's name.

In both ways root is Kallisti's lobby: people pick a voice channel there.

## Running it

With Docker Compose (the Murmur database readable for user 1000):

```sh
docker compose -f deploy/compose.yml stop kallisti
docker compose -f deploy/compose.yml run --rm --no-deps \
  -v /path/to/murmur.sqlite:/import/murmur.sqlite:ro \
  kallisti import-murmur -from /import/murmur.sqlite -dry-run
# Then again without -dry-run, and start Kallisti.
```

Without containers, with the server's settings (`DB_PATH`, `CERT_DIR`):

```sh
kallisti import-murmur -from murmur.sqlite -dry-run
```

Options:

- `-server ID`: only this virtual server (the dry run lists them).
- `-cert`: use the Murmur server's certificate and key for Mumble clients
  (written to `CERT_DIR`, the old ones kept). With the same host name and
  port, clients see no "certificate changed" warning. It must be the
  server's own (in the database); a certificate from Murmur's ini file:
  set `MUMBLE_CERT_FILE` and `MUMBLE_KEY_FILE` to it instead.

## Not imported

- Passwords: users log in with their certificate. Users without a
  certificate (password only) are not imported; they register again.
  SuperUser's password comes from `MUMBLED_SUPERUSER_PASSWORD`.
- Email addresses, channel user limits, the default channel, channel
  listeners and the server log.
- The welcome text: set `MUMBLE_WELCOME`.
- Other virtual servers' ports: Kallisti has one Mumble port, so users of
  the other servers change the port in their client.
