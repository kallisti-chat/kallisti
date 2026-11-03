# Import a Murmur server

## Summary

People who move from Murmur (mumble-server) to Kallisti want their
channels, registered users, groups, ACLs and bans. A friend's database
(two virtual servers, schema version 9) is the test case.

## Requirements

- `server import-murmur -from murmur.sqlite [-server ID] [-cert]`, with
  the Kallisti server stopped, DB_PATH and CERT_DIR as for the server. One
  virtual server per Kallisti; with several and no -server, a list.
- Fresh targets only: no registered Mumble users but SuperUser, no
  channels but Root, Kallisti's automatic General and the text-channel
  mirrors (these stay; the automatic General goes). No web account with an
  imported user's name.
- Imported: the channel tree (names, descriptions, positions, ACL
  inheritance, links), ACLs, groups with members, registered users (name,
  certificate hash, comment, avatar, last channel; Murmur's user ids),
  bans, the server name. Root as on the Murmur server (no lobby added);
  the moderator role is added at the next start.
- Not imported, and said so: passwords (users log in with their
  certificate; SuperUser's comes from MUMBLED_SUPERUSER_PASSWORD), emails,
  the welcome text (MUMBLE_WELCOME), channel user limits, the default
  channel, channel listeners, the server log.
- -cert: Murmur's certificate and key as the server's own (CERT_DIR), so
  clients see no changed certificate; checked first, old files kept.
- A backup of the Kallisti database before writing.

## Acceptance Criteria

- Tests for the reader (a small Murmur database), the import (fresh checks,
  id mapping, mirrors kept) and the command; both virtual servers of the
  test database import, and Kallisti starts with them.
