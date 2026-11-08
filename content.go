package parser

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"
)

const scanChunk = 32 << 10

// ErrInvalidReader indicates a nil reader or an invalid read count.
var ErrInvalidReader = errors.New("parser: invalid reader")

// Line is a value describing one physical line. Number is one-based.
// StartByte and EndByte delimit the half-open source interval [start, end).
// Raw includes the original line ending; Text removes only that ending.
// Raw and Text are views into the source: use strings.Clone when retaining a
// small field should not keep the entire source allocation alive.
type Line struct {
	Number    int
	StartByte int
	EndByte   int
	Raw       string
	Text      string
}

// Content is immutable text indexed by physical lines. Its zero value is empty.
// It can be read concurrently and shared by independent cursors.
type Content struct {
	text   string
	ends   []int
	digest [sha256.Size]byte
}

// New indexes text without trimming whitespace or decoding bytes. It recognizes
// LF, CRLF and CR endings. Empty input has no lines, and a final line ending
// does not create an additional empty line. On error it returns nil Content.
func New(text string, limits Limits) (*Content, error) {
	return NewWithContext(context.Background(), text, limits)
}

// NewWithContext is New with cooperative cancellation during indexing and
// hashing. ctx must be non-nil. Cancellation returns nil Content and ctx.Err().
func NewWithContext(ctx context.Context, text string, limits Limits) (*Content, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if len(text) > limits.MaxBytes {
		return nil, &LimitError{Resource: "bytes", Limit: limits.MaxBytes, Observed: len(text)}
	}

	index := lineIndexer{ends: []int{}, maxLines: limits.MaxLines}
	digest := sha256.New()
	buf := make([]byte, min(len(text), scanChunk))
	for start := 0; start < len(text); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := copy(buf, text[start:])
		if err := index.feed(text[start : start+n]); err != nil {
			return nil, errors.Join(err, ctx.Err())
		}
		if _, err := digest.Write(buf[:n]); err != nil {
			return nil, fmt.Errorf("parser: hash source: %w", err)
		}
		start += n
	}
	var sum [sha256.Size]byte
	digest.Sum(sum[:0])
	return finishContent(ctx, text, &index, sum)
}

// Read reads and indexes r through EOF. It reads at most
// MaxBytes plus one byte, with the extra byte detecting an oversized source.
// It stops reading as soon as the start of an excess physical line is observed.
// Read never closes r. A read error, including io.ErrUnexpectedEOF, returns nil
// Content and an error wrapping the cause. Repeated reads without progress
// return io.ErrNoProgress. Blocking and cancellation are controlled by r.
func Read(r io.Reader, limits Limits) (*Content, error) {
	return ReadWithContext(context.Background(), r, limits)
}

// ReadWithContext is Read with cooperative cancellation. ctx must be non-nil.
// Cancellation is checked before and after reads and between processing chunks.
// It cannot interrupt an already blocked Read: the caller must supply a reader
// with cancellation or deadlines. It never closes r or starts a goroutine.
// Concurrently observed limit, read and cancellation errors remain inspectable
// through errors.Is/As. Every failure returns nil Content.
func ReadWithContext(ctx context.Context, r io.Reader, limits Limits) (*Content, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrInvalidReader
	}

	var text strings.Builder
	buf := make([]byte, min(scanChunk, limits.MaxBytes))
	index := lineIndexer{ends: []int{}, maxLines: limits.MaxLines}
	digest := sha256.New()
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := limits.MaxBytes - text.Len()
		size := min(len(buf), remaining)
		if size == 0 {
			size = 1
		}
		n, readErr := r.Read(buf[:size])
		ctxErr := ctx.Err()
		failure := readFailure(readErr)
		if n < 0 || n > size {
			return nil, errors.Join(ErrInvalidReader, failure, ctxErr)
		}
		if n > remaining {
			limitErr := &LimitError{Resource: "bytes", Limit: limits.MaxBytes, Observed: text.Len() + n}
			return nil, errors.Join(limitErr, failure, ctxErr)
		}
		if ctxErr != nil {
			return nil, errors.Join(ctxErr, failure)
		}
		if n > 0 {
			start := text.Len()
			text.Write(buf[:n])
			// Index the builder's owned bytes without copying the read buffer
			// into a temporary string for each chunk.
			if err := index.feed(text.String()[start:]); err != nil {
				return nil, errors.Join(err, failure, ctx.Err())
			}
			if _, err := digest.Write(buf[:n]); err != nil {
				return nil, errors.Join(fmt.Errorf("parser: hash source: %w", err), failure, ctx.Err())
			}
			emptyReads = 0
		}
		if failure != nil {
			return nil, errors.Join(failure, ctx.Err())
		}
		if readErr == io.EOF {
			var sum [sha256.Size]byte
			digest.Sum(sum[:0])
			return finishContent(ctx, text.String(), &index, sum)
		}
		if n == 0 {
			emptyReads++
			if emptyReads >= 100 {
				return nil, errors.Join(fmt.Errorf("parser: read source: %w", io.ErrNoProgress), ctx.Err())
			}
		}
	}
}

func readFailure(err error) error {
	// io.Reader requires bare EOF for a graceful end. A wrapped or joined EOF
	// must remain a failure, including when errors.Is(err, io.EOF) is true.
	if err == nil || err == io.EOF {
		return nil
	}
	return fmt.Errorf("parser: read source: %w", err)
}

func finishContent(ctx context.Context, text string, index *lineIndexer, digest [sha256.Size]byte) (*Content, error) {
	content := &Content{text: text, ends: index.finish(), digest: digest}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

// String returns the exact original source bytes.
func (c *Content) String() string { return c.text }

// Len returns the number of physical lines.
func (c *Content) Len() int { return len(c.ends) }

// Line returns the one-based line number, or false when number is out of range.
func (c *Content) Line(number int) (Line, bool) {
	if number < 1 || number > len(c.ends) {
		return Line{}, false
	}
	start := 0
	if number > 1 {
		start = c.ends[number-2]
	}
	end := c.ends[number-1]
	raw := c.text[start:end]
	text := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
	return Line{Number: number, StartByte: start, EndByte: end, Raw: raw, Text: text}, true
}

// LineAt returns the physical line containing a zero-based byte offset.
// Line endings belong to the line they terminate. Offsets outside
// [0, len(c.String())) return a zero Line and false, including EOF.
func (c *Content) LineAt(offset int) (Line, bool) {
	if offset < 0 || offset >= len(c.text) {
		return Line{}, false
	}
	i, _ := slices.BinarySearch(c.ends, offset+1)
	return c.Line(i + 1)
}

// All returns a repeatable iterator over every physical line in source order.
// It does not materialize a slice or affect any cursor's position or flags.
func (c *Content) All() iter.Seq[Line] {
	return func(yield func(Line) bool) {
		for n := 1; n <= c.Len(); n++ {
			line, _ := c.Line(n)
			if !yield(line) {
				return
			}
		}
	}
}

// Lines returns all lines in source order in a new slice.
func (c *Content) Lines() []Line {
	lines := make([]Line, c.Len())
	for i := range lines {
		lines[i], _ = c.Line(i + 1)
	}
	return lines
}

// Digest returns the SHA-256 digest of the original bytes, including endings.
// It is an integrity identifier, not a claim that the source has been parsed.
func (c *Content) Digest() [sha256.Size]byte {
	if len(c.text) == 0 {
		return sha256.Sum256(nil)
	}
	return c.digest
}

// Cursor returns a new cursor at the head with every line unparsed.
func (c *Content) Cursor() *Cursor {
	return &Cursor{content: c, parsed: make([]bool, c.Len())}
}
