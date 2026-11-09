package parser

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
)

const scanChunk = 32 << 10

// contentBuilder performs ingestion: it enforces Limits, partitions physical
// lines and computes the digest while a source arrives in contiguous pieces.
// It never retains source text; the caller owns the bytes and passes the
// complete text to finish. A CR completes a physical line immediately; an LF
// at the start of the next piece extends that ending instead of starting
// another line.
type contentBuilder struct {
	ctx     context.Context
	limits  Limits
	ends    []int
	offset  int
	hasCR   bool
	digest  hash.Hash
	scratch []byte
}

// newContentBuilder checks ctx before validating limits, so a cancelled
// context wins over invalid limits. It returns a value so that callers can
// keep the builder off the heap.
func newContentBuilder(ctx context.Context, limits Limits) (contentBuilder, error) {
	if err := ctx.Err(); err != nil {
		return contentBuilder{}, err
	}
	limits, err := limits.normalized()
	if err != nil {
		return contentBuilder{}, err
	}
	return contentBuilder{ctx: ctx, limits: limits, ends: []int{}, digest: sha256.New()}, nil
}

// remaining returns the number of bytes still accepted by the byte limit.
func (b *contentBuilder) remaining() int { return b.limits.MaxBytes - b.offset }

// write ingests the next contiguous piece of the source. The byte limit is
// checked before anything else so that a concurrently observed cancellation
// cannot hide it; cancellation is then checked before every scanChunk step.
func (b *contentBuilder) write(piece string) error {
	if len(piece) > b.remaining() {
		return &LimitError{Resource: ResourceBytes, Limit: b.limits.MaxBytes, Observed: b.offset + len(piece)}
	}
	for len(piece) > 0 {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		step := piece[:min(len(piece), scanChunk)]
		if err := b.index(step); err != nil {
			return err
		}
		if len(b.scratch) < len(step) {
			// SHA-256 accepts only bytes. Grow geometrically to the largest
			// piece seen, capped at scanChunk, never to the whole source.
			b.scratch = make([]byte, min(scanChunk, max(len(step), 2*len(b.scratch))))
		}
		n := copy(b.scratch, step)
		if _, err := b.digest.Write(b.scratch[:n]); err != nil {
			return fmt.Errorf("parser: hash source: %w", err)
		}
		piece = piece[len(step):]
	}
	return nil
}

// index records the line endings in text, which starts at b.offset.
func (b *contentBuilder) index(text string) error {
	base := b.offset
	b.offset += len(text)
	i := 0
	if b.hasCR {
		if text[0] == '\n' {
			b.ends[len(b.ends)-1]++
			i++
		}
		b.hasCR = false
	}
	if i < len(text) && len(b.ends) == b.limits.MaxLines {
		return b.lineLimitError()
	}
	for ; i < len(text); i++ {
		if text[i] != '\n' && text[i] != '\r' {
			continue
		}
		if text[i] == '\r' {
			if i+1 == len(text) {
				b.hasCR = true
			} else if text[i+1] == '\n' {
				i++
			}
		}
		b.ends = append(b.ends, base+i+1)
		if i+1 < len(text) && len(b.ends) == b.limits.MaxLines {
			return b.lineLimitError()
		}
	}
	return nil
}

func (b *contentBuilder) lineLimitError() error {
	return &LimitError{Resource: ResourceLines, Limit: b.limits.MaxLines, Observed: b.limits.MaxLines + 1}
}

// finish closes the final unterminated line and returns the Content for text,
// which must be exactly the bytes written. Cancellation is checked last so
// that a context cancelled during ingestion never yields a Content.
func (b *contentBuilder) finish(text string) (*Content, error) {
	last := 0
	if len(b.ends) > 0 {
		last = b.ends[len(b.ends)-1]
	}
	if last < b.offset {
		b.ends = append(b.ends, b.offset)
	}
	content := &Content{text: text, ends: b.ends}
	b.digest.Sum(content.digest[:0])
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}
