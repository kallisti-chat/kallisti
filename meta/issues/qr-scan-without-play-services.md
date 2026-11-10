# QR scanner without Google Play services

## Summary

tauri-plugin-barcode-scanner uses ML Kit's unbundled scanner (`play-services-mlkit-barcode-scanning`): its model comes from Google Play services, so phones without them (or with an old copy) open the camera but never read a code. Reported by prettygood; the emulator shows the same (`com.google.mlkit.dynamite.barcode not found`).

## Requirements

- Use ML Kit's bundled scanner (`com.google.mlkit:barcode-scanning`, same API) in the Android app, without a plugin fork.

## Acceptance Criteria

- A release APK reads a login code in the emulator, which has no usable Play services model.

## Result (2026-11-10)

Bundled model added (`com.google.mlkit:barcode-scanning:17.3.0`, next to the unbundled API). Verified in the emulator: a real scan from the camera logged in and connected.
