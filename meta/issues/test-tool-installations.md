# Test tooling: remove unused explicit package installations

## Summary

The official-Mumble test image explicitly installs sqlite3, procps, and x11-utils without harness calls to them. The Rust test CI job installs patchelf even though it does not build application bundles.

## Requirements

- Remove those explicit installations only where they have no current role.
- Keep patchelf in desktop packaging and retain media/linking libraries.
- Keep the official-client harness functional in an isolated test environment.

## Acceptance Criteria

- The official-client image builds and its isolated harness still passes.
- The Rust test job no longer installs patchelf; the packaging job still does.
- Syntax and independent source checks pass. If a full GitHub job cannot run locally, record that limit.

## Result (2026-11-09)

Removed explicit sqlite3, procps, and x11-utils from the official-client test image, and patchelf only from the Rust test CI job. Desktop packaging retains patchelf; media and linking libraries are unchanged.

Verification: Both supported Debian image variants built, and all eight isolated official-client tests passed, including the old client. Workflow YAML, harness shell syntax, and diff checks passed. Independent 6.1-sol source review approved the removals. The GitHub Linux Rust job was not run. Unit red-to-green testing was not useful for these declarative removals; actual image and harness checks were used instead. Only task-specific test resources were removed.
