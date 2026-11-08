package parser_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gatework/parser"
)

func TestCursorMultiPass(t *testing.T) {
	const source = "rule allow\r\nobject host\nunknown option\r\n\n"
	content := newContent(t, source)
	cursor := content.Cursor()
	for line, ok := cursor.NextUnparsed(); ok; line, ok = cursor.NextUnparsed() {
		if strings.HasPrefix(line.Text, "object ") {
			if err := cursor.SetParsed(line.Number, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if cursor.UnparsedText() != "rule allow\r\nunknown option\r\n\n" {
		t.Fatal("first pass lost deferred lines or original endings")
	}

	cursor.Rewind()
	seen := []int{}
	for line, ok := cursor.NextUnparsed(); ok; line, ok = cursor.NextUnparsed() {
		seen = append(seen, line.Number)
		if strings.HasPrefix(line.Text, "rule ") {
			if err := cursor.SetParsed(line.Number, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !slices.Equal(seen, []int{1, 3, 4}) || cursor.Position() != 4 {
		t.Fatalf("second pass = %v at position %d", seen, cursor.Position())
	}
	if cursor.UnparsedText() != "unknown option\r\n\n" {
		t.Fatal("unrecognized or empty lines were implicitly consumed")
	}

	if err := cursor.SetParsed(2, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := cursor.NextUnparsed(); ok {
		t.Fatal("marking moved the reader back from eof")
	}
	cursor.Rewind()
	if line, ok := cursor.NextUnparsed(); !ok || line.Number != 2 {
		t.Fatalf("unmarked line was not revisited: %#v, %v", line, ok)
	}
	if content.String() != source || content.Cursor().UnparsedText() != source {
		t.Fatal("cursor state escaped into the content")
	}
}

func TestCursorNavigation(t *testing.T) {
	cursor := newContent(t, "a\n\nc\r\n").Cursor()
	if cursor.Position() != 0 || cursor.Prev() {
		t.Fatal("invalid head position")
	}
	if line, ok := cursor.Peek(); !ok || line.Number != 1 || cursor.Position() != 0 {
		t.Fatal("peek moved the cursor")
	}
	cursor.Next()
	checkpoint := cursor.Checkpoint()
	for _, number := range []int{1, 2} {
		if err := cursor.SetParsed(number, true); err != nil {
			t.Fatal(err)
		}
	}
	if line, ok := cursor.Next(); !ok || line.Number != 2 || line.Text != "" {
		t.Fatal("Next did not return the parsed empty physical line")
	}
	if line, ok := cursor.NextUnparsed(); !ok || line.Number != 3 {
		t.Fatal("next unparsed line is wrong")
	}
	for range 2 {
		if line, ok := cursor.Next(); ok || line != (parser.Line{}) || cursor.Position() != 3 {
			t.Fatal("Next changed position at eof")
		}
		if line, ok := cursor.NextUnparsed(); ok || line != (parser.Line{}) || cursor.Position() != 3 {
			t.Fatal("NextUnparsed changed position at eof")
		}
	}
	if !cursor.Prev() || cursor.Position() != 2 {
		t.Fatal("Prev failed at eof")
	}
	if line, ok := cursor.Peek(); !ok || line.Number != 3 {
		t.Fatal("Prev did not move one physical line")
	}
	for range 2 {
		if err := cursor.Restore(checkpoint); err != nil || cursor.Position() != 1 {
			t.Fatalf("restore: %v, position %d", err, cursor.Position())
		}
		if line, ok := cursor.NextUnparsed(); !ok || line.Number != 3 {
			t.Fatal("restore changed parsed flags")
		}
	}
	cursor.Rewind()
	if line, ok := cursor.NextUnparsed(); !ok || line.Number != 3 {
		t.Fatal("rewind changed parsed flags")
	}
	if cursor.UnparsedText() != "c\r\n" {
		t.Fatal("reading changed parsed flags")
	}
	if err := cursor.SetParsed(3, true); err != nil {
		t.Fatal(err)
	}
	cursor.Rewind()
	if line, ok := cursor.NextUnparsed(); ok || line != (parser.Line{}) || cursor.Position() != 3 {
		t.Fatal("all-parsed scan did not reach eof")
	}
}

func TestCursorInvalidCheckpoint(t *testing.T) {
	content := newContent(t, "a\nb\n")
	tests := []struct {
		name       string
		checkpoint parser.Checkpoint
	}{
		{name: "zero"},
		{name: "same content other cursor", checkpoint: content.Cursor().Checkpoint()},
		{name: "different content", checkpoint: newContent(t, "x").Cursor().Checkpoint()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursor := content.Cursor()
			cursor.Next()
			if err := cursor.SetParsed(1, true); err != nil {
				t.Fatal(err)
			}
			if err := cursor.Restore(tt.checkpoint); !errors.Is(err, parser.ErrCheckpoint) {
				t.Fatalf("restore error = %v", err)
			}
			if cursor.Position() != 1 || cursor.UnparsedText() != "b\n" {
				t.Fatal("failed restore changed state")
			}
		})
	}
}

func TestCursorFlags(t *testing.T) {
	cursor := newContent(t, "a\nb").Cursor()
	cursor.Next()
	for _, flag := range []bool{true, true, false, false} {
		if err := cursor.SetParsed(1, flag); err != nil {
			t.Fatal(err)
		}
		if got, err := cursor.IsParsed(1); err != nil || got != flag || cursor.Position() != 1 {
			t.Fatalf("flag = %v, error = %v, position = %d", got, err, cursor.Position())
		}
	}
	for _, number := range []int{-1, 0, 3, int(^uint(0) >> 1)} {
		if err := cursor.SetParsed(number, true); !errors.Is(err, parser.ErrLine) {
			t.Fatalf("SetParsed(%d) error = %v", number, err)
		}
		if flag, err := cursor.IsParsed(number); flag || !errors.Is(err, parser.ErrLine) {
			t.Fatalf("IsParsed(%d) = %v, %v", number, flag, err)
		}
	}
	if cursor.Position() != 1 || cursor.UnparsedText() != "a\nb" {
		t.Fatal("invalid flag operation changed the cursor")
	}
}

func TestCursorUnparsedIsolation(t *testing.T) {
	cursor := newContent(t, "a\r\nb\rc").Cursor()
	pending := cursor.UnparsedLines()
	pending[0].Raw = "changed"
	if cursor.UnparsedText() != "a\r\nb\rc" {
		t.Fatal("returned slice changed source")
	}
	if err := cursor.SetParsed(2, true); err != nil {
		t.Fatal(err)
	}
	if pending[1].Raw != "b\r" || cursor.UnparsedText() != "a\r\nc" {
		t.Fatal("remaining source or prior snapshot changed")
	}
	lines := cursor.UnparsedLines()
	if len(lines) != 2 || lines[0].Number != 1 || lines[1].Number != 3 {
		t.Fatal("unparsed lines lost source numbering")
	}
	for _, number := range []int{1, 3} {
		if err := cursor.SetParsed(number, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(cursor.UnparsedLines()) != 0 || cursor.UnparsedText() != "" {
		t.Fatal("fully parsed source still has unparsed text")
	}
}

func TestCursorZeroValue(t *testing.T) {
	var cursor parser.Cursor
	checkpoint := cursor.Checkpoint()
	if line, ok := cursor.Next(); ok || line != (parser.Line{}) {
		t.Fatal("empty cursor returned a line")
	}
	if _, ok := cursor.NextUnparsed(); ok || cursor.Prev() {
		t.Fatal("empty cursor moved")
	}
	cursor.Rewind()
	if err := cursor.Restore(checkpoint); err != nil {
		t.Fatal(err)
	}
	if cursor.Position() != 0 || cursor.UnparsedText() != "" || len(cursor.UnparsedLines()) != 0 {
		t.Fatal("zero cursor is not empty")
	}
	if err := cursor.SetParsed(1, true); !errors.Is(err, parser.ErrLine) {
		t.Fatalf("empty cursor accepted a flag: %v", err)
	}
}

func TestContentConcurrentCursors(t *testing.T) {
	const source = "one\ntwo\nthree\nfour\n"
	content := newContent(t, source)
	digest := content.Digest()
	for worker := range 8 {
		t.Run(string(rune('a'+worker)), func(t *testing.T) {
			t.Parallel()
			cursor := content.Cursor()
			chosen := worker%content.Len() + 1
			if cursor.UnparsedText() != source {
				t.Fatal("new cursor inherited another cursor's flags")
			}
			if err := cursor.SetParsed(chosen, true); err != nil {
				t.Fatal(err)
			}
			count := 0
			for line, ok := cursor.NextUnparsed(); ok; line, ok = cursor.NextUnparsed() {
				if line.Number == chosen {
					t.Fatal("parsed line returned")
				}
				count++
			}
			if count != 3 || content.Digest() != digest || content.String() != source {
				t.Fatal("shared content changed")
			}
		})
	}
}

func TestCursorSetParsedRange(t *testing.T) {
	cursor := newContent(t, "one\ntwo\nthree").Cursor()
	cursor.Next()
	if err := cursor.SetParsed(2, true); err != nil {
		t.Fatal(err)
	}
	for _, interval := range [][2]int{{0, 2}, {1, 4}, {3, 2}, {-1, 1}, {4, 4}} {
		if err := cursor.SetParsedRange(interval[0], interval[1], true); !errors.Is(err, parser.ErrLine) {
			t.Fatalf("invalid range accepted: %v, %v", interval, err)
		}
		if cursor.UnparsedText() != "one\nthree" || cursor.Position() != 1 {
			t.Fatal("invalid range partially changed state")
		}
	}
	for range 2 {
		if err := cursor.SetParsedRange(1, 3, true); err != nil {
			t.Fatal(err)
		}
		if cursor.UnparsedText() != "" || cursor.Position() != 1 {
			t.Fatal("valid range did not mark all lines or changed position")
		}
	}
	if err := cursor.SetParsedRange(2, 3, false); err != nil {
		t.Fatal(err)
	}
	if cursor.UnparsedText() != "two\nthree" {
		t.Fatal("range reset did not restore lines")
	}
	var empty parser.Cursor
	if err := empty.SetParsedRange(1, 1, true); !errors.Is(err, parser.ErrLine) {
		t.Fatal("empty cursor accepted a range")
	}
}

func TestCursorUnparsedIterator(t *testing.T) {
	cursor := newContent(t, "one\ntwo\nthree\nfour").Cursor()
	cursor.Next()
	if err := cursor.SetParsed(2, true); err != nil {
		t.Fatal(err)
	}
	unparsed := cursor.Unparsed()
	numbers := []int{}
	for line := range unparsed {
		numbers = append(numbers, line.Number)
		if line.Number == 1 {
			if err := cursor.SetParsed(2, false); err != nil {
				t.Fatal(err)
			}
			if err := cursor.SetParsed(3, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !slices.Equal(numbers, []int{1, 2, 4}) || cursor.Position() != 1 {
		t.Fatalf("live iteration = %v, position = %d", numbers, cursor.Position())
	}
	if err := cursor.SetParsed(1, true); err != nil {
		t.Fatal(err)
	}
	if got := slices.Collect(unparsed); !slices.Equal(got, cursor.UnparsedLines()) || len(got) != 2 {
		t.Fatal("repeat traversal did not observe current flags")
	}
	for line := range unparsed {
		if line.Number != 2 {
			t.Fatal("early-stop traversal started at the wrong line")
		}
		break
	}
	if cursor.Position() != 1 || cursor.UnparsedText() != "two\nfour" {
		t.Fatal("iterator changed position or flags")
	}
	var empty parser.Cursor
	for range empty.Unparsed() {
		t.Fatal("empty cursor yielded a line")
	}
}
