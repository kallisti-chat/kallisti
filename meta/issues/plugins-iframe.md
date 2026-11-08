# Plugins: the sandboxed page (only if needed)

## Summary

For a plugin that needs more than the panel vocabulary: a page served
through the proxy, shown in a sandboxed iframe with a small `postMessage`
bridge. Do this only when a real plugin needs it.

## Requirements

- `frame-src 'self'` in the page policy; `frame-ancestors 'none'` stays.
- `sandbox` without `allow-same-origin`; the proxy adds
  `Content-Security-Policy: sandbox` to plugin pages, as `internal/files`.
- The bridge answers only the capabilities in the manifest
  (`sendChat`, `currentChannel`, `currentUser`), with only that data.
- Theme variables passed in; size set by the panel.

## Acceptance Criteria

- Go test: the headers. Vitest: the bridge refuses everything not declared.
- Manual check: a test page cannot read the session or call other routes.
