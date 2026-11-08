# Build: run emoji-data generation on Windows and spaced paths

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

The script compares import.meta.url with an unescaped file:// string made from argv. Encoded spaces and Windows paths fail the entry check, silently omitting the untracked emoji asset.

Priority: Medium.

Evidence: A checkout path containing spaces exited successfully without generating data; Windows mismatch is source-confirmed.

## Requirements

- Normalize the CLI path as a file URL or separate the CLI entry point.
- Make the build fail if required output is absent.

## Acceptance Criteria

- A subprocess test from a path with spaces generates data.
- Windows path/entry handling is tested, and an import does not unexpectedly run the CLI.
- The generated emoji asset is present in a clean web build.

## Notes

- apps/web/scripts/emojidata.mjs:43-49
- .github/workflows/desktop.yml

## Result (2026-11-08)

A dedicated CLI writes the required asset unconditionally, without comparing argv paths to file URLs. Importing the pure conversion helpers does not write files. Build and dev stop if generation fails.

Verification: Three subprocess regressions failed before the fix and now pass: spaced checkout paths, Windows-style argv independence, and write failure. Helper import remains side-effect free. A fresh asset build produced 1,914 entries in dist/emoji-data.json. Windows execution was simulated, not run on Windows. Full workspace tests/type checks and the web build passed. Independent 6.1-sol and Astra reviews approved the fix.
