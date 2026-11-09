# Web tooling: remove the unused TypeScript compiler alias

## Summary

The web package declares TypeScript 7 under @typescript/native alongside TypeScript 6. Its typecheck uses svelte-check and TypeScript 6. No web script selects the alias, but it can claim the local tsc command.

## Requirements

- Remove the unused alias from the web package and lockfile.
- Keep the compiler used by svelte-check and the TypeScript 7 requirements of other workspaces.

## Acceptance Criteria

- A regression detects the redundant alias before removal and passes afterward.
- Frozen installation, workspace tests/typechecks, and the web build pass.
