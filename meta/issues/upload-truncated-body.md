# Uploads: reject interrupted request bodies

Parent: [Code audit follow-ups](code-audit-followups.md)

## Summary

The initial content-sniff read discards its error. After a short interrupted body, the later copy can see EOF and succeed, so a partial file is stored as a successful upload.

Priority: Medium.

Evidence: A local raw HTTP probe declared 1,000 bytes, sent 100, and received HTTP 200.

## Requirements

- Preserve read errors through sniffing and copying, and validate a known body length.
- Failed requests must leave no upload row or file. Keep existing size limits.

## Acceptance Criteria

- Short and longer interrupted bodies are rejected, including an interruption within the first 512 bytes.
- Successful small and large uploads, empty-body policy, limits, and cleanup tests pass.

## Notes

- server/internal/files/files.go:179-205,247-250
