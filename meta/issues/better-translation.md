# Better translation: Hy-MT2 on the device (WebGPU)

## Requirements

- Bergamot is nearly useless for Chinese (tests 2026-11-06). Hy-MT2-1.8B (Tencent, Apache 2.0, a translation model) with a background prompt (the app, the messages before) and a glossary was right on the test lines; about 2.8 s per message on a CPU, less with WebGPU.
- A setting "Better translation", off by default, only with WebGPU: Hy-MT2 for all languages, Bergamot when it fails.
- The model (ONNX q4f16 build `DavidLuong/Hy-MT2-1.8B-ONNX-q4f16`, pinned revision, 1.4 GB) comes only through our server (fetched once, SHA-256 checked, kept), as Bergamot's.
- The ONNX runtime's files are bundled (transformers.js loads them from a CDN by default).
- Prompt: Tencent's background template with one sentence on the app and the 2-3 messages before; a glossary made for each message: app words (per language) that are in the message, and names (users, channels) kept as they are.
- Progress during the first download.

## Acceptance Criteria

- Unit tests: the prompt, the glossary, the server's model files.
- Checked in a browser with WebGPU against a local server.
