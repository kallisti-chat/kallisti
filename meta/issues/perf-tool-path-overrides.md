# Performance tests: honor explicit tool paths during prerequisite checks

## Summary

The performance harness supports FFMPEG and SQLITE3 overrides when seeding, but its initial checks still require the default commands on PATH. Valid explicit executable paths are therefore rejected.

## Requirements

- Check the configured executable when an override is supplied, otherwise check the default command.
- Keep tool checks conditional on seeding.
- Fail clearly for an invalid override without starting browsers or live services.

## Acceptance Criteria

- Regressions first fail for valid off-PATH overrides, then pass after the fix.
- Cover default PATH tools, spaced explicit paths, invalid overrides, and SEED=0.
- Existing harness syntax and relevant tests pass without live services or media devices.

## Result (2026-11-09)

The seeding prerequisite checks now use each configured executable, including paths with spaces, and check default names only when an override is unset or empty. Invalid overrides fail before work-directory changes or builds. SEED=0 skips the checks.

Verification: Sixteen restricted-PATH regressions first had ten failures and now all pass. All eighteen devtools tests passed on Node 24 and 26 after integration; shell syntax and diff checks passed. The new POSIX tests skip on Windows. Independent 6.1-sol review approved the change. Tests stop before builds or services; no full performance run was needed.
