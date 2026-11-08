package parser

import (
	"errors"
	"fmt"
	"iter"
	"strings"
)

var (
	// ErrLine indicates a line number outside the cursor's source.
	ErrLine = errors.New("parser: line number out of range")
	// ErrCheckpoint indicates an uninitialized or foreign checkpoint.
	ErrCheckpoint = errors.New("parser: invalid checkpoint")
)

// Cursor owns a read position and per-line parsed flags. The zero value is an
// empty cursor. A Cursor must not be copied after first use or used concurrently.
// Reading, rewinding and restoring never change parsed flags.
type Cursor struct {
	noCopy   noCopy
	content  *Content
	parsed   []bool
	position int
}

// noCopy lets go vet identify accidental copies of a stateful Cursor.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Next returns the next physical line, including parsed and empty lines.
// At EOF it returns a zero Line and false without changing position.
func (c *Cursor) Next() (Line, bool) {
	line, ok := c.Peek()
	if ok {
		c.position++
	}
	return line, ok
}

// Peek returns the next physical line without advancing the cursor.
func (c *Cursor) Peek() (Line, bool) {
	if c.content == nil {
		return Line{}, false
	}
	return c.content.Line(c.position + 1)
}

// NextUnparsed returns the next unparsed line, including empty lines, advancing
// past it. Parsed lines are skipped. No line's parsed flag is changed.
func (c *Cursor) NextUnparsed() (Line, bool) {
	for c.position < len(c.parsed) {
		i := c.position
		c.position++
		if !c.parsed[i] {
			return c.content.Line(i + 1)
		}
	}
	return Line{}, false
}

// Position returns the zero-based index of the next physical line.
// At EOF it equals the source's line count.
func (c *Cursor) Position() int { return c.position }

// Prev moves back one physical line, returning false if already at the head.
// It does not search for the previous unparsed line or undo any parsed flag.
func (c *Cursor) Prev() bool {
	if c.position == 0 {
		return false
	}
	c.position--
	return true
}

// Rewind moves to the head, preserving all parsed flags.
func (c *Cursor) Rewind() { c.position = 0 }

// Checkpoint is a saved position belonging to one Cursor. Its zero value is
// invalid. It contains no snapshot of parsed flags and can be restored repeatedly.
type Checkpoint struct {
	owner    *Cursor
	position int
}

// Checkpoint saves the current read position.
func (c *Cursor) Checkpoint() Checkpoint {
	return Checkpoint{owner: c, position: c.position}
}

// Restore restores a position saved by this cursor without changing parsed
// flags. An invalid checkpoint returns ErrCheckpoint without changing position.
func (c *Cursor) Restore(checkpoint Checkpoint) error {
	if checkpoint.owner != c {
		return ErrCheckpoint
	}
	if checkpoint.position < 0 || checkpoint.position > len(c.parsed) {
		return ErrCheckpoint
	}
	c.position = checkpoint.position
	return nil
}

// SetParsed changes the flag for a one-based line number. Repeating a value is
// harmless; false makes the line available to a later NextUnparsed scan.
// The read position is unchanged, including when the number is invalid.
func (c *Cursor) SetParsed(line int, parsed bool) error {
	if line < 1 || line > len(c.parsed) {
		return fmt.Errorf("%w: %d", ErrLine, line)
	}
	c.parsed[line-1] = parsed
	return nil
}

// SetParsedRange sets the flags in the inclusive, one-based [first, last]
// interval. It validates the entire interval before changing any flags.
// An empty, reversed or out-of-bounds interval returns ErrLine. Position is
// unchanged, and the cursor's single-goroutine ownership still applies.
func (c *Cursor) SetParsedRange(first, last int, parsed bool) error {
	if first < 1 || last < first || last > len(c.parsed) {
		return fmt.Errorf("%w: range [%d, %d]", ErrLine, first, last)
	}
	for i := first - 1; i < last; i++ {
		c.parsed[i] = parsed
	}
	return nil
}

// IsParsed reports the flag for a one-based line number, or ErrLine if invalid.
func (c *Cursor) IsParsed(line int) (bool, error) {
	if line < 1 || line > len(c.parsed) {
		return false, fmt.Errorf("%w: %d", ErrLine, line)
	}
	return c.parsed[line-1], nil
}

// Unparsed returns an iterator over unparsed lines, independent of position.
// Each traversal examines the current flags in source order. Flag changes in
// the loop affect lines not yet visited; earlier lines are not revisited until
// the next traversal. The iterator never changes position or flags itself.
func (c *Cursor) Unparsed() iter.Seq[Line] {
	return func(yield func(Line) bool) {
		for i := range c.parsed {
			if !c.parsed[i] {
				line, _ := c.content.Line(i + 1)
				if !yield(line) {
					return
				}
			}
		}
	}
}

// UnparsedLines returns a new slice of all unparsed lines in source order,
// independent of the read position. It includes unparsed empty lines.
func (c *Cursor) UnparsedLines() []Line {
	count := 0
	for _, isParsed := range c.parsed {
		if !isParsed {
			count++
		}
	}
	lines := make([]Line, 0, count)
	for i, isParsed := range c.parsed {
		if !isParsed {
			line, _ := c.content.Line(i + 1)
			lines = append(lines, line)
		}
	}
	return lines
}

// UnparsedText joins the Raw bytes of every unparsed line in source order.
// It neither changes position nor introduces or normalizes line endings.
func (c *Cursor) UnparsedText() string {
	size := 0
	for i, isParsed := range c.parsed {
		if !isParsed {
			start := 0
			if i > 0 {
				start = c.content.ends[i-1]
			}
			size += c.content.ends[i] - start
		}
	}
	var text strings.Builder
	text.Grow(size)
	for i, isParsed := range c.parsed {
		if !isParsed {
			start := 0
			if i > 0 {
				start = c.content.ends[i-1]
			}
			text.WriteString(c.content.text[start:c.content.ends[i]])
		}
	}
	return text.String()
}
