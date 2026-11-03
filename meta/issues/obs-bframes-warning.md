# OBS: tell the streamer about B-frames

## Summary

The server forwards OBS's H.264 as it comes, and viewers decode pictures
in arrival order: B-frames break the picture (a user saw pink and a
purple bar). The server can see B slices in the slice headers and tell
the streamer how to turn them off, instead of a broken picture.

## Requirements

- whip: find B slices (slice_type 1 or 6) in each picture; at the first
  one of a stream, log it and tell the owner's voice session once.
- sidecar: a notice to one session; the web app shows it as a toast,
  long enough to read.

## Acceptance Criteria

- Tests: hand-made slice headers (I, P, B, all-B, short), a real x264
  clip with B-frames, the clip without them; one warning per stream; the
  notice reaches only its session; the client parses and shows it.
