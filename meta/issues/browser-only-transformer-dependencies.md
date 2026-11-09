# Translation: omit unused native Node backends

## Summary

Browser-only Transformers.js translation pulls in onnxruntime-node and sharp. Kallisti does not use those Node backends. The ONNX Node package also declares no FreeBSD support.

## Requirements

- Exclude only the native Node backends pulled in by @huggingface/transformers.
- Keep Transformers.js, its browser ONNX runtime, and other consumers unchanged.
- Remove obsolete install-script policy and lockfile entries if no consumer remains.
- Do not claim FreeBSD or old-CPU support without a platform test.

## Acceptance Criteria

- A regression first detects the installed Node backends and passes after removal.
- A clean frozen web installation has neither native backend in its dependency graph.
- Translation checks, workspace tests/typechecks, and the production web build pass without the native packages.

## Result (2026-11-09)

Targeted pnpm overrides remove only the native ONNX and sharp dependency edges from Transformers.js. The browser ONNX runtime remains. Removed the unused native-backend install policy and lockfile graph entries.

Verification: The native-backend regressions failed before the change and all three now pass. A clean filtered frozen installation, typecheck, and browser build passed without either native package installed. Main workspace tests/typechecks and web build passed. Independent 6.1-sol review approved the change. FreeBSD and old-CPU execution were not tested.
