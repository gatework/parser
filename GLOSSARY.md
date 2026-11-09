# parser

Immutable source text indexed by physical lines, read through independent
cursors that record which lines a caller has parsed across multiple passes.

## Source

**Content**:
Immutable source bytes together with their physical-line partition and Digest.
_Avoid_: config, document, file

**Line**:
One physical line of a Content: its one-based number, byte interval, and raw
bytes with and without the line ending.
_Avoid_: row, record

**Span**:
The exact source bytes of an inclusive interval of consecutive Lines.
_Avoid_: block, section (those are caller syntax)

**Ingestion**:
Turning source bytes into a Content: enforcing Limits, partitioning lines and
computing the Digest, all-or-nothing.
_Avoid_: loading, parsing, indexing (indexing is only one part)

**Limits**:
The maximum bytes and physical lines Ingestion accepts.
_Avoid_: quota, caps

**Digest**:
The SHA-256 identity of a Content's exact source bytes.
_Avoid_: signature, checksum, confSign

## Reading

**Cursor**:
One reader's position over a Content plus its own parsed flags.
_Avoid_: iterator, scanner, reader

**Parsed flag**:
A per-line, per-Cursor mark that a caller successfully parsed that line.
Reading never sets it.
_Avoid_: consumed, handled

**Position**:
The zero-based index of the next Line a Cursor reads, from 0 to the line
count, which is EOF. Seek sets it directly.
_Avoid_: offset (offsets are bytes), index

**Checkpoint**:
A saved Cursor position, restorable only on the Cursor that made it; it does
not capture parsed flags.
_Avoid_: snapshot, savepoint
