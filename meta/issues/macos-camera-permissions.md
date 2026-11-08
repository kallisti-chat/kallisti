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
