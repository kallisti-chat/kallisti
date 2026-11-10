# Performance tests: validate executable paths across POSIX shells

## Summary

GitHub run 42758701064 fails four performance prerequisite regressions on Ubuntu. Its /bin/sh can return success from command -v for an explicit path that names a directory or non-executable file. The Mac shell rejects those paths, so local checks did not expose the difference.

## Requirements

- Require the configured tool to resolve to an executable regular file, not merely exist.
- Keep valid PATH commands, explicit/spaced overrides, and SEED=0 behavior unchanged.
- Retain the failing invalid-file and directory regressions; do not weaken them or skip Linux.
- Do not change the deployed UI code or unrelated CI jobs.

## Acceptance Criteria

- Reproduce the existing four failures under a Linux /bin/sh before the fix.
- All prerequisite regressions pass under Linux /bin/sh and the Mac shell after the fix.
- Workspace JavaScript tests/typechecks and independent review pass.

## Notes

- https://github.com/kallisti-chat/kallisti/actions/runs/42758701064
- Related: perf-tool-path-overrides.md

## Result (2026-11-10)

Prerequisite checks now require a resolved executable regular file. They no longer depend on shell-specific command -v behavior for explicit paths. The fixture accepts PERF_TEST_SHELL so dash can be checked locally without weakening the existing cases.

Verification: The four failures from the Ubuntu GitHub job were reproduced locally with dash before the source fix. All sixteen prerequisite regressions now pass under dash, Bash, and the Mac default shell; syntax checks pass for all three. Workspace JavaScript tests and typechecks passed. Independent 6.1-sol review approved the fix. Tests stop before builds, browsers, or services.
