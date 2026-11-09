# Changelog

## v0.2.0

2026-11-09

Breaking release; no compatibility shims are provided.

- `Content.String()` is replaced by `Content.Text()`. `Content` now implements
  `fmt.Formatter` and `slog.LogValuer`, printing only line count, byte count and
  digest, so formatting or logging a Content never discloses the source.
- `ErrLine` is renamed `ErrLineRange`; it also reports invalid `Seek` positions.
- `LimitError.Resource` has type `Resource`, with `ResourceBytes` and `ResourceLines`.
- New `Cursor.Seek`, `Cursor.Content`, `Cursor.UnparsedCount` and `Content.Span`.
- Parsed flags are stored as a bitset: cursor memory is one bit per line, and
  scans skip fully parsed words. See [performance](docs/performance.md).

## v0.1.1

2026-11-08

Internal change only; exported API, errors, limits and cancellation behavior are unchanged.

- `New` and `Read` share one ingestion path for limits, line indexing, digest and
  cancellation, so both constructors apply the same rules by construction.

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
