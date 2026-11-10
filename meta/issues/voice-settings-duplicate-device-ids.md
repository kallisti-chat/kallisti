# Voice settings: tolerate empty and duplicate device IDs

## Summary

The Voice and Video settings panel keys raw browser device lists by deviceId. Empty or duplicate IDs cause Svelte each_key_duplicate and leave the panel blank. The report at https://pastebin.com/wzLyvRvT has the same exception; isolated component tests reproduce it with empty and repeated microphone or output IDs.

## Requirements

- Normalize selectable microphones, speakers, and cameras before keyed rendering.
- Exclude empty IDs and the default alias already represented by the default option, and remove duplicate IDs within each device kind.
- Keep the default choices and the camera permission hint.
- Test with fake device enumeration only; never open real media devices.

## Acceptance Criteria

- Regressions fail with empty/duplicate IDs before the change and pass afterward.
- The voice category can be opened repeatedly as device enumeration changes.
- The same ID remains valid across different device kinds; default and named choices remain usable.
- Relevant component tests, workspace tests/typechecks, and the web build pass.

## Result (2026-11-10)

Voice settings now build selectable device lists per kind before keyed rendering. Empty/default aliases and duplicate IDs are omitted, while named choices, cross-kind IDs, and the camera permission hint remain available.

Verification: Four component regressions failed before the fix; all six device-list tests now pass. Full workspace tests/typechecks and the production web build passed. Independent 6.1-sol review approved the fix. Tests use synthetic enumeration and never open real media devices; the reporter hardware list remains unknown.
