# Import a Murmur server

## Summary

People who move from Murmur (mumble-server) to Kallisti want their
channels, registered users, groups, ACLs and bans. A friend's database
(two virtual servers, schema version 9) is the test case.

## Requirements

- `server import-murmur -from murmur.sqlite [-server ID] [-cert]`, with
  the Kallisti server stopped, DB_PATH and CERT_DIR as for the server.
- Without -server: every virtual server as a channel under Root (named
  after it), its Root ACL and groups on that channel; user ids renumbered,
  one certificate is one user (memberships of both kept), a name taken by
  an earlier server's user gets a suffix ("name-2"), listed. With -server
  ID: that server alone, its Root as Kallisti's Root (its admins are
  Kallisti's admins, no lobby added).
- Fresh targets only: no registered Mumble users but SuperUser, no
  channels but Root, Kallisti's automatic General and the text-channel
  mirrors (these stay; the automatic General goes). No web account with an
  imported user's name.
- Imported: the channel tree (names, descriptions, positions, ACL
  inheritance, links), ACLs, groups with members, registered users (name,
  certificate hash, comment, avatar, last channel; Murmur's user ids),
  bans, the server name (-server only). The moderator role is added at the
  next start.
- Not imported, and said so: passwords (users log in with their
  certificate; SuperUser's comes from MUMBLED_SUPERUSER_PASSWORD), emails,
  the welcome text (MUMBLE_WELCOME), channel user limits, the default
  channel, channel listeners, the server log.
- -cert: Murmur's certificate and key as the server's own (CERT_DIR), so
  clients see no changed certificate (without -server: the first
  server's); checked first, old files kept.
- A backup of the Kallisti database before writing.

## Acceptance Criteria

- Tests for the reader (a small Murmur database), the plan (both modes,
  merges, renames), the import (fresh checks, id mapping, mirrors kept)
  and the command; the test database imports in both modes, and Kallisti
  starts with it.
