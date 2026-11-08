package parser_test

import (
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gatework/parser"
)

func FuzzCursor(f *testing.F) {
	// Opcode, line number and flag have independent bytes. The two-byte line
	// operand can address every line in the bounded source, including past 255.
	f.Add("a\r\nb\n\nc", []byte{5, 0, 2, 1, 0, 0, 0, 0, 6, 0, 0, 0, 1, 0, 0, 0, 7, 0, 0, 0})
	f.Add("", []byte{0, 0, 0, 0, 5, 0, 1, 1, 8, 0, 0, 0, 9, 0, 0, 0})
	f.Add("中文\r\xff\n", []byte{5, 0, 1, 1, 1, 0, 0, 0, 4, 0, 0, 0, 2, 0, 0, 0})
	for _, count := range []int{3, 8, 300} {
		f.Add(strings.Repeat("x\n", count), []byte{
			5, byte(count >> 8), byte(count), 1, // Mark the final line.
			9, 0, 0, 0,
			5, byte(count >> 8), byte(count), 0, // Clear the same flag.
			1, 0, 0, 0,
			3, 0, 0, 0,
			1, 0, 0, 0,
		})
	}
	f.Fuzz(func(t *testing.T, source string, operations []byte) {
		if len(source) > 2048 || len(operations) > 512 {
			t.Skip()
		}
		content := newContent(t, source)
		lines := content.Lines()
		cursor := content.Cursor()
		parsed := map[int]bool{}
		checkpoint := cursor.Checkpoint()
		savedPosition := 0

		for i := 0; i+3 < len(operations); i += 4 {
			op := operations[i] % 10
			number := int(binary.BigEndian.Uint16(operations[i+1:i+3])) % (len(lines) + 2)
			flag := operations[i+3]&1 != 0
			before := cursor.Position()
			wantPosition := before
			switch op {
			case 0:
				line, ok := cursor.Next()
				if before == len(lines) {
					if ok || line != (parser.Line{}) {
						t.Fatal("Next returned data at eof")
					}
				} else {
					if !ok || line != lines[before] {
						t.Fatal("Next returned the wrong physical line")
					}
					wantPosition++
				}
			case 1:
				line, ok := cursor.NextUnparsed()
				wantPosition = len(lines)
				if ok {
					if line.Number <= before || line.Number > len(lines) {
						t.Fatal("NextUnparsed returned a line outside the remaining source")
					}
					if parsed[line.Number] || line != lines[line.Number-1] {
						t.Fatal("NextUnparsed returned an incorrect or parsed line")
					}
					wantPosition = line.Number
				} else if line != (parser.Line{}) {
					t.Fatal("NextUnparsed returned nonzero data at eof")
				}
				for number := before + 1; number <= wantPosition; number++ {
					if ok && number == line.Number {
						break
					}
					if !parsed[number] {
						t.Fatal("NextUnparsed skipped an unparsed line")
					}
				}
			case 2:
				line, ok := cursor.Peek()
				if before == len(lines) {
					if ok || line != (parser.Line{}) {
						t.Fatal("Peek returned data at eof")
					}
				} else if !ok || line != lines[before] {
					t.Fatal("Peek returned the wrong line")
				}
			case 3:
				if cursor.Prev() != (before > 0) {
					t.Fatal("Prev reported the wrong boundary")
				}
				wantPosition = max(0, before-1)
			case 4:
				cursor.Rewind()
				wantPosition = 0
			case 5:
				err := cursor.SetParsed(number, flag)
				if number < 1 || number > len(lines) {
					if !errors.Is(err, parser.ErrLine) {
						t.Fatal("invalid flag operation succeeded")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					parsed[number] = flag
				}
			case 6:
				checkpoint = cursor.Checkpoint()
				savedPosition = before
			case 7:
				if err := cursor.Restore(checkpoint); err != nil {
					t.Fatal(err)
				}
				wantPosition = savedPosition
			case 8:
				if err := cursor.Restore(content.Cursor().Checkpoint()); !errors.Is(err, parser.ErrCheckpoint) {
					t.Fatal("foreign checkpoint was accepted")
				}
			case 9:
				if !slices.Equal(slices.Collect(cursor.Unparsed()), cursor.UnparsedLines()) {
					t.Fatal("iterator and snapshot disagree")
				}
			}
			if cursor.Position() != wantPosition {
				t.Fatalf("operation %d: position = %d, want %d", op, cursor.Position(), wantPosition)
			}
			var unparsed strings.Builder
			for _, line := range lines {
				flag, err := cursor.IsParsed(line.Number)
				if err != nil || flag != parsed[line.Number] {
					t.Fatal("operation implicitly changed a parsed flag")
				}
				if !parsed[line.Number] {
					unparsed.WriteString(line.Raw)
				}
			}
			if cursor.UnparsedText() != unparsed.String() {
				t.Fatal("unparsed source was lost or modified")
			}
		}
	})
}
