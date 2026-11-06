# Translate messages on the device

## Requirements

- A user asked for automatic translation (Chinese and English speakers in one chat). The server has no GPU: translation runs on the reader's device.
- Phase 1: "Translate" in the message menu. The translation shows under the original, into the app's language. Engines: the browser's Translator API where it exists (desktop Chrome, Edge), else Bergamot (Firefox's engine, WebAssembly, `@mkljczk/bergamot-translator`), with Mozilla's models.
- Models (user, 2026-11-06): our server downloads a pair once from the Hugging Face mirror `midudev/firefox-translations` (pinned revision, SHA-256 checked) and keeps it; clients get them only from our server. Nobody else sees who translates what.
- Phase 2 (later): translate automatically what is not in my language. Later: an optional larger model on WebGPU.

## Acceptance Criteria

- Server: model list and files, checked, cached, tests with a fake upstream.
- Client: language detection and engine choice with unit tests; the menu item and the shown translation; texts in all 7 languages.
- Checked in headless Chromium: a Chinese message translated to English through our server.
