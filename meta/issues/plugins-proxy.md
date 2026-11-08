# Plugins: the proxy, the configuration and the user name

## Summary

The server forwards `/api/plugins/<id>/*` to a configured plugin (not
`/api/v1/`: the login provider owns it). It checks who may use the plugin,
adds the secret and the user name, and passes the event stream.

## Requirements

- Environment variables, indexed (`PLUGINS=gotamusique`,
  `PLUGIN_GOTAMUSIQUE_URL`, `_TOKEN`, `_ACCESS`, `_CHANNELS`); checks in
  `config.Parse`: id characters and uniqueness (and not clashing with
  other routes), http or https URL without user info, token not empty;
  docs in `docs/configuration.md`, tests in `config_test.go`, and the
  compose, Helm and systemd files.
- The proxy is pinned to the configured host and port: no other target,
  no redirects followed, no `..` in paths. Not `netguard.Public`: the bot
  is on loopback and the admin sets the URL.
- Streaming: flush per chunk, no total timeout, stop the upstream when the
  browser leaves, a cap on open streams per plugin; other requests have
  time-outs and a body limit.
- Strips the browser's `Authorization` and cookies; adds
  `Authorization: Bearer <token>` and `X-Kallisti-User` from the session
  (`accounts.AccountFor`). The token is never logged.
- Access `members` (default) and `everyone` checked on every request.
- A plugin that is down: 502 with a short message, and Kallisti keeps
  running.

## Acceptance Criteria

- Go tests with a fake upstream: forwarding, header stripping and adding,
  the event stream, access, bad config, path tricks, upstream down.
- The token does not appear in a response, a log line or the web bundle.
