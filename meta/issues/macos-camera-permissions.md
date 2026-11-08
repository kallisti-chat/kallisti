# Desktop macOS: declare camera privacy permissions

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

Camera sharing is offered where the encoder is supported, but the macOS privacy description and hardened-runtime entitlements only declare microphone access.

Priority: Medium.

Evidence: Source finding; signed native runtime behavior has not been tested.

## Requirements

- Add the camera usage description and camera entitlement to the configured macOS bundle files.
- Do not access the real camera in automated tests.

## Acceptance Criteria

- Static configuration checks cover the camera declarations and configured plist paths.
- Verify the signed macOS build can request camera access using an approved manual or fake-device test; leave this issue open if that cannot be checked.

## Notes

- apps/desktop/src-tauri/Info.plist:4
- apps/desktop/src-tauri/Entitlements.plist:4
- apps/desktop/src-tauri/tauri.conf.json:22

## Progress (2026-11-08)

Added the camera usage description and hardened-runtime camera entitlement to the configured bundle files. The implementation is committed, but the issue remains open: signed-app permission behavior still needs an approved runtime check.

Verification: Both static declaration tests failed before the fix and now pass. Configured plist paths and existing microphone declarations are checked; plist lint passed. Independent 6.1-sol and Astra reviews approved the static changes only. No camera, microphone, or GUI app was opened.
