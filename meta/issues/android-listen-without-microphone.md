# Android: background playback without the microphone permission

## Summary

The Android app keeps voice running in the background with a foreground service of type `microphone`. It starts only when the microphone is allowed. A user who only listens and denies the microphone gets no service: Android freezes the app about 5 s after it goes to the background (seen in the emulator: `isFrozen=true`, silence). On phones that throttle instead of freezing, this may cause the "fast, stop, fast" playback reported by thatBrickster.

## Requirements

- Without the microphone permission, start the service as type `mediaPlayback`.
- With the permission, keep type `microphone` (recording in the background needs it).
- Declare `FOREGROUND_SERVICE_MEDIA_PLAYBACK`.

## Acceptance Criteria

- Emulator, release APK, microphone denied: the service runs as `mediaPlayback` and playback in the background stays smooth for several minutes.
- Microphone allowed: still type `microphone`.

## Result (2026-11-12)

Service type by permission (mediaPlayback / microphone), started at once, upgraded in front. Both acceptance criteria checked in the emulator with a debug-signed release APK.
