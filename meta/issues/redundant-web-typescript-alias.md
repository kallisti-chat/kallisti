# Web tooling: remove the unused TypeScript compiler alias

## Summary

The web package declares TypeScript 7 under @typescript/native alongside TypeScript 6. Its typecheck uses svelte-check and TypeScript 6. No web script selects the alias, but it can claim the local tsc command.

## Requirements

- Remove the unused alias from the web package and lockfile.
- Keep the compiler used by svelte-check and the TypeScript 7 requirements of other workspaces.

## Acceptance Criteria

- A regression detects the redundant alias before removal and passes afterward.
- Frozen installation, workspace tests/typechecks, and the web build pass.

## Result (2026-11-09)

Removed the unused @typescript/native alias from the web manifest and lockfile. The web compiler and svelte-check use TypeScript 6; protocol and devtools still use their required TypeScript 7.

Verification: The alias regression failed before removal and both compiler-policy tests now pass. Frozen installation, workspace tests/typechecks, and the web build passed. The web tsc shim now resolves to TypeScript 6. Independent 6.1-sol review approved the change.
