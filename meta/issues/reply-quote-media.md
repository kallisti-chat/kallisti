# Replies: show the quoted message's picture

## Summary

A reply's quote shows only author and text. When the answered message has a picture or video, the quote should show a small version of it (feld, #kallisti-dev).

## Requirements

- The server adds the first attachment of the answered message to the quote (its small picture: the preview, or the image itself; a video's poster), with the same URLs (signed links included) as the message.
- Spoilers (`SPOILER_` names) give no picture.
- The client shows a small thumbnail in the quote line.

## Acceptance Criteria

- Store, wire and client tests for a quote with a picture, a video, a spoiler and a plain file.
- Visible in the web app for new replies and in loaded history.

## Result (2026-11-12)

Done as specified; private-room signed links of quote pictures are refreshed too. Edits of the original do not update existing quotes (as for the quote text).
