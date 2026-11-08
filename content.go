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
	builder, err := newContentBuilder(ctx, limits)
	if err != nil {
		return nil, err
	}
	if err := builder.write(text); err != nil {
		return nil, joinFailure(err, ctx.Err())
	}
	return builder.finish(text)
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
	builder, err := newContentBuilder(ctx, limits)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrInvalidReader
	}

	var text strings.Builder
	buf := make([]byte, min(scanChunk, builder.remaining()))
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// One byte past the limit detects an oversized source.
		size := max(min(len(buf), builder.remaining()), 1)
		n, readErr := r.Read(buf[:size])
		failure := readFailure(readErr)
		if n < 0 || n > size {
			return nil, joinFailure(ErrInvalidReader, failure, ctx.Err())
		}
		if n > 0 {
			start := text.Len()
			text.Write(buf[:n])
			// Ingest the builder's owned bytes without copying the read buffer
			// into a temporary string for each chunk.
			if err := builder.write(text.String()[start:]); err != nil {
				return nil, joinFailure(err, failure, ctx.Err())
			}
			emptyReads = 0
		} else if err := ctx.Err(); err != nil {
			return nil, joinFailure(err, failure)
		}
		if failure != nil {
			return nil, joinFailure(failure, ctx.Err())
		}
		if readErr == io.EOF {
			return builder.finish(text.String())
		}
		if n == 0 {
			emptyReads++
			if emptyReads >= 100 {
				return nil, joinFailure(fmt.Errorf("parser: read source: %w", io.ErrNoProgress), ctx.Err())
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

// joinFailure keeps every error observed at the same time inspectable through
// errors.Is/As. It omits nil causes and causes err already contains, and
// returns err itself when nothing else was observed.
func joinFailure(err error, causes ...error) error {
	errs := []error{err}
	for _, cause := range causes {
		if cause != nil && !errors.Is(err, cause) {
			errs = append(errs, cause)
		}
	}
	if len(errs) == 1 {
		return err
	}
	return errors.Join(errs...)
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
