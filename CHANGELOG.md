# Changelog

## v0.1.0

2026-11-07

Initial release of `github.com/gatework/parser`, a standard-library-only Go module
for lossless, line-oriented, multi-pass configuration parsing. Requires Go 1.26+.

- Immutable source text, physical line indexing, byte-offset lookup and SHA-256.
- Independent cursors with explicit parsed flags, checkpoints and validated range updates.
- Repeatable `All` and `Unparsed` iterators, plus line and text snapshots.
- Incremental reader processing with byte and line limits, including split CRLF endings.
- Cooperative context cancellation and structured, inspectable error chains.
- External-package tests, examples, fuzz coverage and recorded performance comparisons.

Syntax recognition, character decoding and device models belong to callers.
Context cancellation does not interrupt an already blocked Reader.
