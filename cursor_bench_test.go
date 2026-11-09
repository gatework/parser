package parser_test

import (
	"testing"

	"github.com/gatework/parser"
)

func BenchmarkCursorNext(b *testing.B) {
	for _, name := range []string{"small", "short_lines", "long_line"} {
		b.Run(name, func(b *testing.B) {
			content, err := parser.New(benchmarkSource(name), parser.Limits{})
			if err != nil {
				b.Fatal(err)
			}
			cursor := content.Cursor()
			b.ReportAllocs()
			for b.Loop() {
				cursor.Rewind()
				total := 0
				for line, ok := cursor.Next(); ok; line, ok = cursor.Next() {
					total += len(line.Text)
				}
				benchmarkTotal = total
			}
		})
	}
}

func benchmarkUnparsedCursor(b *testing.B, name, density string) *parser.Cursor {
	b.Helper()
	content, err := parser.New(benchmarkSource(name), parser.Limits{})
	if err != nil {
		b.Fatal(err)
	}
	cursor := content.Cursor()
	for n := 1; n <= content.Len(); n++ {
		var isParsed bool
		switch density {
		case "sparse":
			isParsed = n%10 != 0
		case "rare":
			isParsed = n%100 != 0
		default:
			isParsed = n%10 == 0
		}
		if err := cursor.SetParsed(n, isParsed); err != nil {
			b.Fatal(err)
		}
	}
	return cursor
}

func BenchmarkUnparsedLines(b *testing.B) {
	for _, name := range []string{"small", "short_lines"} {
		for _, density := range []string{"rare", "sparse", "dense"} {
			b.Run(name+"/"+density, func(b *testing.B) {
				cursor := benchmarkUnparsedCursor(b, name, density)
				b.ReportAllocs()
				for b.Loop() {
					total := 0
					for _, line := range cursor.UnparsedLines() {
						total += len(line.Text)
					}
					benchmarkTotal = total
				}
			})
		}
	}
}

func BenchmarkUnparsedText(b *testing.B) {
	for _, name := range []string{"small", "short_lines"} {
		for _, density := range []string{"rare", "sparse", "dense"} {
			b.Run(name+"/"+density, func(b *testing.B) {
				cursor := benchmarkUnparsedCursor(b, name, density)
				b.ReportAllocs()
				for b.Loop() {
					benchmarkTotal = len(cursor.UnparsedText())
				}
			})
		}
	}
}

func BenchmarkUnparsed(b *testing.B) {
	for _, name := range []string{"small", "short_lines"} {
		for _, density := range []string{"rare", "sparse", "dense"} {
			b.Run(name+"/"+density, func(b *testing.B) {
				cursor := benchmarkUnparsedCursor(b, name, density)
				b.ReportAllocs()
				for b.Loop() {
					total := 0
					for line := range cursor.Unparsed() {
						total += len(line.Text)
					}
					benchmarkTotal = total
				}
			})
		}
	}
}

func BenchmarkNextUnparsed(b *testing.B) {
	for _, name := range []string{"small", "short_lines"} {
		for _, density := range []string{"rare", "sparse", "dense"} {
			b.Run(name+"/"+density, func(b *testing.B) {
				cursor := benchmarkUnparsedCursor(b, name, density)
				b.ReportAllocs()
				for b.Loop() {
					cursor.Rewind()
					total := 0
					for line, ok := cursor.NextUnparsed(); ok; line, ok = cursor.NextUnparsed() {
						total += len(line.Text)
					}
					benchmarkTotal = total
				}
			})
		}
	}
}

func BenchmarkContentCursor(b *testing.B) {
	for _, name := range []string{"small", "short_lines"} {
		b.Run(name, func(b *testing.B) {
			content, err := parser.New(benchmarkSource(name), parser.Limits{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				benchmarkTotal = content.Cursor().UnparsedCount()
			}
		})
	}
}

func BenchmarkSetParsedRange(b *testing.B) {
	content, err := parser.New(benchmarkSource("short_lines"), parser.Limits{})
	if err != nil {
		b.Fatal(err)
	}
	cursor := content.Cursor()
	b.ReportAllocs()
	for b.Loop() {
		if err := cursor.SetParsedRange(1, content.Len(), true); err != nil {
			b.Fatal(err)
		}
		if err := cursor.SetParsedRange(1, content.Len(), false); err != nil {
			b.Fatal(err)
		}
	}
}
