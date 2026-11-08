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
