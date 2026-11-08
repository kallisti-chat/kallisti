# Plugins: the manifest and the declarative panel

## Summary

Fetch the manifest through the proxy, check it, and draw the panel with
Kallisti's own components. Live state from the event stream, with polling
as the fallback.

## Requirements

- `lib/plugins/`: manifest checks (version, size, depth, component count),
  paths and `{ }` binding, conditions, action resolution. Pure functions
  with tests; components stay thin.
- Components as in the manifest: stack, section, text, image, button,
  buttonGroup, slider, select, toggle, list, search, form, commandForm,
  divider. An unknown type draws "not supported".
- Text only, never HTML. Images only from the plugin's paths.
- Manifest cached with `ETag`; state through the event stream while the
  panel is open and none otherwise; polling when the stream drops.
- Errors as toasts; "not reachable" when the plugin is down.
- Theme variables, keyboard use, phone layout.

## Acceptance Criteria

- Vitest: every check, the binding rules, each component's action.
- The gotamusique example in the manifest document draws and works against
  a fake plugin.
- Manual check with the real bot: skip, volume and mode from the panel; a
  `!skip` typed in chat shows in the panel without a reload.
