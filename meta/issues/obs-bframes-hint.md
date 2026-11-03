# OBS: how to turn off B-frames

## Summary

A user's OBS stream (30.1.2, x264, profile baseline) came out pink with a
purple bar until they put `bframes=0` in "x264 Options". The OBS dialog
said only "no B-frames", and x264 in OBS has no B-frames field.

## Requirements

- The OBS dialog and docs/configuration.md say how: x264 `bframes=0` in
  "x264 Options"; NVENC, QuickSync, AMF: B-Frames 0.

## Acceptance Criteria

- The dialog and the docs have the per-encoder advice.
