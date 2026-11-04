# Server texts as codes, translated in the client

## Summary

The client is localized (see localization.md), but texts the server sends
stay English: refusals ("Only admins can delete users."), manage errors,
notices (OBS B-frames), API errors on login and registration. The server
should send a code with values, and the client translates it, with the
English text as a fallback for older clients and Mumble apps.

## Acceptance Criteria

- Sidecar errors, notices and API errors have codes; the client shows them
  in the chosen language.
- Mumble apps still get English text (PermissionDenied reasons).
