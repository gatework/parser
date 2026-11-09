package parser

import (
	"errors"
	"fmt"
	"iter"
	"math/bits"
	"strings"
)

var (
	// ErrLineRange indicates a line number, interval or position outside the
	// cursor's source.
	ErrLineRange = errors.New("parser: line out of range")
	// ErrCheckpoint indicates an uninitialized or foreign checkpoint.
	ErrCheckpoint = errors.New("parser: invalid checkpoint")
)

// Cursor owns a read position and per-line parsed flags. The zero value is an
// empty cursor. A Cursor must not be copied after first use or used concurrently.
// Reading, rewinding and restoring never change parsed flags.
type Cursor struct {
	noCopy   noCopy
	content  *Content
	parsed   []uint64 // Bit i%64 of word i/64 is the flag of line index i.
	unparsed int
	position int
}

// newCursor returns a cursor at the head of content with every line unparsed.
// Padding bits past the last line are permanently set, so scans never treat
// them as unparsed lines.
func newCursor(content *Content) *Cursor {
	n := content.Len()
	parsed := make([]uint64, (n+63)/64)
	if pad := n % 64; pad != 0 {
		parsed[len(parsed)-1] = ^uint64(0) << pad
	}
	return &Cursor{content: content, parsed: parsed, unparsed: n}
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
	i := c.nextUnparsed(c.position)
	if i == c.lineCount() {
		c.position = i
		return Line{}, false
	}
	c.position = i + 1
	return c.content.Line(i + 1)
}

// lineCount returns the number of lines, which is zero for the zero Cursor.
func (c *Cursor) lineCount() int {
	if c.content == nil {
		return 0
	}
	return c.content.Len()
}

// nextUnparsed returns the first unparsed index at or after from, or the line
// count when none remains. Words with every flag set are skipped whole.
func (c *Cursor) nextUnparsed(from int) int {
	n := c.lineCount()
	for from < n {
		w := from / 64
		if free := ^c.parsed[w] >> (from % 64); free != 0 {
			return from + bits.TrailingZeros64(free)
		}
		from = (w + 1) * 64
	}
	return n
}

// isParsed reports the flag at zero-based index i, which must be in range.
func (c *Cursor) isParsed(i int) bool {
	return c.parsed[i/64]&(1<<(i%64)) != 0
}

// setRange sets the flags of the zero-based half-open interval [lo, hi), a
// word at a time, and maintains the unparsed count.
func (c *Cursor) setRange(lo, hi int, parsed bool) {
	for lo < hi {
		w, shift := lo/64, lo%64
		n := min(64-shift, hi-lo)
		mask := ^uint64(0) >> (64 - n) << shift
		old := c.parsed[w]
		if parsed {
			c.parsed[w] = old | mask
			c.unparsed -= bits.OnesCount64(^old & mask)
		} else {
			c.parsed[w] = old &^ mask
			c.unparsed += bits.OnesCount64(old & mask)
		}
		lo += n
	}
}

// Position returns the zero-based index of the next physical line.
// At EOF it equals the source's line count.
func (c *Cursor) Position() int { return c.position }

// Seek moves to a zero-based position in [0, Len], the same domain as
// [Cursor.Position]; Len is EOF. Seeking to line.Number-1 makes line the next
// one read, for example after [Content.LineAt]. An invalid position returns
// ErrLineRange without changing position. Parsed flags are unchanged.
func (c *Cursor) Seek(position int) error {
	if position < 0 || position > c.lineCount() {
		return fmt.Errorf("%w: position %d", ErrLineRange, position)
	}
	c.position = position
	return nil
}

// Content returns the source this cursor reads, or nil for the zero Cursor.
func (c *Cursor) Content() *Content { return c.content }

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
	if checkpoint.position < 0 || checkpoint.position > c.lineCount() {
		return ErrCheckpoint
	}
	c.position = checkpoint.position
	return nil
}

// SetParsed changes the flag for a one-based line number. Repeating a value is
// harmless; false makes the line available to a later NextUnparsed scan.
// The read position is unchanged, including when the number is invalid.
func (c *Cursor) SetParsed(number int, parsed bool) error {
	if number < 1 || number > c.lineCount() {
		return fmt.Errorf("%w: %d", ErrLineRange, number)
	}
	c.setRange(number-1, number, parsed)
	return nil
}

// SetParsedRange sets the flags in the inclusive, one-based [first, last]
// interval. It validates the entire interval before changing any flags.
// An empty, reversed or out-of-bounds interval returns ErrLineRange. Position is
// unchanged, and the cursor's single-goroutine ownership still applies.
func (c *Cursor) SetParsedRange(first, last int, parsed bool) error {
	if first < 1 || last < first || last > c.lineCount() {
		return fmt.Errorf("%w: range [%d, %d]", ErrLineRange, first, last)
	}
	c.setRange(first-1, last, parsed)
	return nil
}

// IsParsed reports the flag for a one-based line number, or ErrLineRange if
// invalid.
func (c *Cursor) IsParsed(number int) (bool, error) {
	if number < 1 || number > c.lineCount() {
		return false, fmt.Errorf("%w: %d", ErrLineRange, number)
	}
	return c.isParsed(number - 1), nil
}

// UnparsedCount returns the number of unparsed lines in constant time. It is
// zero when every line has been parsed.
func (c *Cursor) UnparsedCount() int { return c.unparsed }

// unparsedIndexes yields the zero-based index of every unparsed line in
// order, visiting the clear bits of each flag word. The current word is read
// again after each yield, so flags changed by the consumer affect every index
// not yet visited.
func (c *Cursor) unparsedIndexes() iter.Seq[int] {
	return func(yield func(int) bool) {
		for w := range c.parsed {
			free := ^c.parsed[w]
			for free != 0 {
				bit := bits.TrailingZeros64(free)
				i := w*64 + bit
				if !yield(i) {
					return
				}
				free = ^c.parsed[w] & (^uint64(0) << bit << 1)
			}
		}
	}
}

// Unparsed returns an iterator over unparsed lines, independent of position.
// Each traversal examines the current flags in source order. Flag changes in
// the loop affect lines not yet visited; earlier lines are not revisited until
// the next traversal. The iterator never changes position or flags itself.
func (c *Cursor) Unparsed() iter.Seq[Line] {
	return func(yield func(Line) bool) {
		for i := range c.unparsedIndexes() {
			line, _ := c.content.Line(i + 1)
			if !yield(line) {
				return
			}
		}
	}
}

// UnparsedLines returns a new slice of all unparsed lines in source order,
// independent of the read position. It includes unparsed empty lines.
func (c *Cursor) UnparsedLines() []Line {
	lines := make([]Line, 0, c.unparsed)
	for i := range c.unparsedIndexes() {
		line, _ := c.content.Line(i + 1)
		lines = append(lines, line)
	}
	return lines
}

// unparsedRuns yields maximal half-open index intervals [lo, hi) of
// consecutive unparsed lines in order. Callers must not change flags while
// ranging over it.
func (c *Cursor) unparsedRuns() iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		lo, hi := 0, 0
		for w, word := range c.parsed {
			for free := ^word; free != 0; {
				bit := bits.TrailingZeros64(free)
				length := bits.TrailingZeros64(^(free >> bit))
				start := w*64 + bit
				if start != hi {
					if hi > lo && !yield(lo, hi) {
						return
					}
					lo = start
				}
				hi = start + length
				free &^= ^uint64(0) >> (64 - length) << bit
			}
		}
		if hi > lo {
			yield(lo, hi)
		}
	}
}

// UnparsedText joins the Raw bytes of every unparsed line in source order.
// It neither changes position nor introduces or normalizes line endings.
func (c *Cursor) UnparsedText() string {
	size := 0
	for lo, hi := range c.unparsedRuns() {
		start, _ := c.content.bounds(lo)
		size += c.content.ends[hi-1] - start
	}
	var text strings.Builder
	text.Grow(size)
	for lo, hi := range c.unparsedRuns() {
		start, _ := c.content.bounds(lo)
		text.WriteString(c.content.text[start:c.content.ends[hi-1]])
	}
	return text.String()
}
