-- A small Murmur database for tests: Murmur's schema (version 9, as
-- mumble-server 1.4 and 1.5 write it) and two servers.
-- Two servers. 1 "Main": Lobby (1) with Games (2) under it, linked to
-- Music (3); root ACL for @admin, a group with alice; alice (cert aa,
-- email, comment), bob (cert bb, a password), carol (no cert: password
-- only); an IPv4 ban. 2 "Side": Chat (1); alice again (cert aa, other
-- name), "Bob" with another cert, dave.
CREATE TABLE `servers` (`server_id` INTEGER PRIMARY KEY AUTOINCREMENT);
CREATE TABLE `channels` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `parent_id` INTEGER, `name` TEXT, `inheritacl` INTEGER);
CREATE TABLE `channel_info` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `key` INTEGER, `value` TEXT);
CREATE TABLE `channel_links` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `link_id` INTEGER NOT NULL);
CREATE TABLE `acl` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `priority` INTEGER, `user_id` INTEGER, `group_name` TEXT, `apply_here` INTEGER, `apply_sub` INTEGER, `grantpriv` INTEGER, `revokepriv` INTEGER);
CREATE TABLE `groups` (`group_id` INTEGER PRIMARY KEY AUTOINCREMENT, `server_id` INTEGER NOT NULL, `name` TEXT, `channel_id` INTEGER NOT NULL, `inherit` INTEGER, `inheritable` INTEGER);
CREATE TABLE `group_members` (`group_id` INTEGER NOT NULL, `server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `addit` INTEGER);
CREATE TABLE `users` (`server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `name` TEXT NOT NULL, `pw` TEXT, `salt` TEXT, `kdfiterations` INTEGER, `lastchannel` INTEGER, `texture` BLOB, `last_active` DATE, `last_disconnect` DATE);
CREATE TABLE `user_info` (`server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `key` INTEGER, `value` TEXT);
CREATE TABLE `bans` (`server_id` INTEGER NOT NULL, `base` BLOB, `mask` INTEGER, `name` TEXT, `hash` TEXT, `reason` TEXT, `start` DATE, `duration` INTEGER);
CREATE TABLE `config` (`server_id` INTEGER NOT NULL, `key` TEXT, `value` TEXT);
CREATE TABLE `channel_listeners` (`server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `volume_adjustment` FLOAT DEFAULT 1, `enabled` SMALLINT DEFAULT 1);
INSERT INTO servers VALUES (1), (2);
INSERT INTO config VALUES (1, 'registername', 'Main'), (1, 'welcometext', 'Hello'), (2, 'registername', 'Side'), (2, 'defaultchannel', '1');
INSERT INTO channels VALUES (1, 0, NULL, 'Root', 1), (1, 1, 0, 'Lobby', 1), (1, 2, 1, 'Games', 0), (1, 3, 0, 'Music', 1), (2, 0, NULL, 'Root', 1), (2, 1, 0, 'Chat', 1);
INSERT INTO channel_info VALUES (1, 0, 0, 'Root text'), (1, 1, 1, '5'), (1, 2, 0, 'Play'), (1, 3, 2, '10');
INSERT INTO channel_links VALUES (1, 2, 3), (1, 3, 2);
INSERT INTO acl VALUES (1, 0, 1, NULL, 'admin', 1, 1, 0x1 | 0x10000, 0), (1, 2, 1, 2, NULL, 1, 0, 0x8, 0x4), (2, 0, 1, NULL, 'admin', 1, 1, 0x1, 0);
INSERT INTO groups VALUES (1, 1, 'admin', 0, 1, 1), (2, 2, 'admin', 0, 1, 1);
INSERT INTO group_members VALUES (1, 1, 1, 1), (1, 1, 2, 0), (2, 2, 3, 1);
INSERT INTO users (server_id, user_id, name, pw, lastchannel, texture) VALUES
  (1, 0, 'SuperUser', 'x', 0, NULL), (1, 1, 'alice', NULL, 2, NULL), (1, 2, 'bob', 'abc', 0, NULL), (1, 3, 'carol', 'abc', 0, NULL),
  (2, 0, 'SuperUser', 'x', 0, NULL), (2, 1, 'alice2', NULL, 1, NULL), (2, 2, 'Bob', NULL, 0, NULL), (2, 3, 'dave', NULL, 1, NULL);
INSERT INTO user_info VALUES (1, 1, 3, 'aa'), (1, 1, 1, 'alice@example.org'), (1, 1, 2, 'hi'), (1, 2, 3, 'bb'),
  (2, 1, 3, 'aa'), (2, 2, 3, 'b2'), (2, 3, 3, 'dd');
INSERT INTO bans VALUES (1, x'00000000000000000000ffff0a000000', 104, 'eve', 'ee', 'spam', '2019-01-02T03:04:05Z', 3600);
INSERT INTO channel_listeners (server_id, user_id, channel_id) VALUES (1, 1, 3);
