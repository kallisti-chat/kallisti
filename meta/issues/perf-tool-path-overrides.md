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
