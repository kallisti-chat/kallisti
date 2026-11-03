# Security audit (GitHub #10)

## Summary

SHSauler's audit against dc4aa3c: nothing exploitable; hardening items.
Finding 2 (nosniff on all responses) is done (PR #13).

## Requirements

- CI: every action pinned to a full commit SHA (the tag in a comment),
  Dependabot keeps them current; every workflow has an explicit, least
  `permissions` block (helm, voice and test had none: the repository
  default, read, applied, but scanners count that as write-all).
- Helper images (deploy/docker/Dockerfile.caddy-dns01, Dockerfile.cert-sync):
  a non-root user and a health check, without breaking existing volumes.
- Production: ALLOWED_ORIGINS is exactly the site's origin.
- Confirm or fix: session cookies Secure on HTTPS; SHA-1 and math/rand only
  for non-security uses; the exported Android activity; old Rust crates in
  the desktop app.

## Acceptance Criteria

- CI green on the pinned workflows; images build and start healthy; each
  confirmation answered with the code that shows it.
