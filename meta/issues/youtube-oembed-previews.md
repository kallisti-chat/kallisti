# YouTube link previews through oEmbed

## Summary

Fetch YouTube video metadata from its fixed oEmbed endpoint instead of relying on watch-page HTML, which can lack metadata for server requests.

## Requirements

- Recognize supported video URLs on an explicit list of YouTube hosts and validate the video ID.
- Request the fixed HTTPS oEmbed endpoint first for recognized videos, without API keys, cookies or browser automation.
- Show a normal title/thumbnail preview card; ignore returned embed HTML.
- Keep current fetch deadlines, size bounds, SSRF checks and server-side image proxying.
- Leave generic HTML preview handling and general cache/retry behavior unchanged.

## Acceptance criteria

- Local tests cover URL forms, host/ID validation, bounded JSON parsing, failed lookups, thumbnail proxying and unsafe thumbnail URLs.
- Existing preview tests and relevant server race tests and vet pass.
- Independent code and security reviews approve the change.

## Notes

Related approach: https://akkoma.dev/AkkomaGang/akkoma/pulls/1168. Implement the behavior in Go without copying the AGPL patch.
