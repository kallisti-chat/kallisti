# Devtools: remove the Bun requirement

## Summary

The optional smoke and voicebot commands run through Bun even though the rest of the JavaScript tooling uses Node and pnpm. Bun is difficult to package on FreeBSD and limits builds on older hardware. The README also lists it as a general development requirement.

## Requirements

- Run both tools with Node through the existing pnpm commands, without Bun.
- Preserve TypeScript protocol imports and existing command arguments.
- Update current setup and usage documentation; keep historical notes intact.
- Test with local fixtures only, without live services or real media devices.

## Acceptance Criteria

- Both package commands work when Bun is unavailable.
- The smoke tool receives and reports a synced protocol state.
- The voice bot reads a synthetic Ogg Opus fixture and sends voice packets.
- Workspace tests and typechecks pass; the lockfile records the replacement runner.
- Current instructions no longer require Bun. FreeBSD runtime support is not claimed without a platform test.

## Result (2026-11-08)

Both commands now use `node --import tsx` through pnpm. The runner is a locked development dependency. esbuild uses its optional platform binary without an install script. Current setup and usage instructions no longer require Bun; historical notes are unchanged.

Verification: Both local-fixture tests failed with the old Bun commands and now pass on Node 24.15.0 and 26.13.0, including a Node 24 run with Bun absent from PATH. They cover synced state, Origin, the bot name, a spaced file path, and outbound Opus packets. Frozen installation, workspace tests/typechecks, and the web build passed. Independent 6.1-sol review approved the change. No live service or media device was used; FreeBSD and Westmere execution remains untested.
