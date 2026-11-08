package parser_test

import (
	"crypto/sha256"
	"io"
	"strings"
	"testing"

	"github.com/gatework/parser"
)

func FuzzContent(f *testing.F) {
	for _, seed := range []string{"", "x", "\n", "\r\n\r\n", "a\r\nb\rc\n", " \t中文\r\n", "\xff\x00\xfe"} {
		f.Add(seed, []byte{0})
		f.Add(seed, []byte{1, 2, 0, 255})
	}
	f.Fuzz(func(t *testing.T, source string, chunks []byte) {
		if len(source) > 64<<10 || len(chunks) > 256 {
			t.Skip()
		}
		limits := parser.Limits{MaxBytes: 64 << 10, MaxLines: 64 << 10}
		content, err := parser.New(source, limits)
		if err != nil {
			t.Fatal(err)
		}
		position, step := 0, 0
		reader := readFunc(func(p []byte) (int, error) {
			if position == len(source) {
				return 0, io.EOF
			}
			size := len(p)
			if len(chunks) > 0 {
				size = min(size, int(chunks[step%len(chunks)])+1)
				step++
			}
			n := copy(p[:size], source[position:])
			position += n
			if position == len(source) && len(chunks) > 0 && chunks[0]&1 != 0 {
				return n, io.EOF
			}
			return n, nil
		})
		streamed, err := parser.Read(reader, limits)
		if err != nil {
			t.Fatal(err)
		}
		if streamed.String() != source || streamed.Len() != content.Len() || streamed.Digest() != content.Digest() {
			t.Fatal("reader chunk boundaries changed source, line count or digest")
		}
		var joined strings.Builder
		end, number := 0, 0
		for line := range content.All() {
			number++
			if line.Number != number || line.StartByte != end || line.EndByte <= end || line.EndByte > len(source) {
				t.Fatalf("invalid source partition: %#v", line)
			}
			if got, ok := streamed.Line(number); !ok || got != line {
				t.Fatal("reader chunk boundaries changed a line")
			}
			for _, offset := range []int{line.StartByte, line.EndByte - 1} {
				if got, ok := content.LineAt(offset); !ok || got != line {
					t.Fatal("byte offset did not identify its line")
				}
			}
			if line.Raw != source[line.StartByte:line.EndByte] || !strings.HasPrefix(line.Raw, line.Text) {
				t.Fatal("line no longer identifies its original bytes")
			}
			if strings.ContainsAny(line.Text, "\r\n") {
				t.Fatal("Text contains a line ending")
			}
			ending := line.Raw[len(line.Text):]
			switch ending {
			case "":
				if line.Number != content.Len() {
					t.Fatal("unterminated line was not the final line")
				}
			case "\n", "\r", "\r\n":
			default:
				t.Fatalf("Text removed source other than the line ending: %q", ending)
			}
			joined.WriteString(line.Raw)
			end = line.EndByte
		}
		if joined.String() != source || end != len(source) || content.String() != source || number != content.Len() {
			t.Fatal("line partition did not preserve the source")
		}
		if content.Digest() != sha256.Sum256([]byte(source)) {
			t.Fatal("digest no longer identifies the source")
		}
		cursor := content.Cursor()
		for _, ok := cursor.NextUnparsed(); ok; _, ok = cursor.NextUnparsed() {
		}
		if cursor.UnparsedText() != source || cursor.Position() != content.Len() {
			t.Fatal("reading consumed source or failed to reach eof")
		}
	})
}
