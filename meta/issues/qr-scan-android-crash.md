# QR scan: the Android release app crashes when the camera opens

## Requirements

- User report (Samsung Galaxy A34, One UI 8.5): "Scan a QR code" opens
  the camera, then the app closes at once.
- Reproduced with the v0.13.0 release APK in the API 36 emulator: a
  NullPointerException in ML Kit, from `scanner.process` in the
  plugin's `analyze` (R8-minified code).
- Release builds only: they run R8 (optimization enabled), debug builds
  do not.

## Acceptance Criteria

- A local release APK opens the camera in the emulator without a crash.
- The user checks scanning on a phone with the next release.
