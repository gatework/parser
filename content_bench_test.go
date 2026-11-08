package parser_test

import (
	"strings"
	"testing"

	"github.com/gatework/parser"
)

var benchmarkTotal int

func benchmarkSource(name string) string {
	switch name {
	case "small":
		return strings.Repeat("key=value      \n", 64)
	case "short_lines":
		return strings.Repeat("set option value\n", 200_000)
	case "long_line":
		return strings.Repeat("x", parser.DefaultMaxBytes-2) + "\r\n"
	default:
		panic("unknown benchmark source")
	}
}

func BenchmarkNew(b *testing.B) {
	for _, name := range []string{"small", "short_lines", "long_line"} {
		b.Run(name, func(b *testing.B) {
			source := benchmarkSource(name)
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			for b.Loop() {
				content, err := parser.New(source, parser.Limits{})
				if err != nil || content == nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRead(b *testing.B) {
	for _, name := range []string{"small", "short_lines", "long_line"} {
		b.Run(name, func(b *testing.B) {
			source := benchmarkSource(name)
			reader := strings.NewReader(source)
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			for b.Loop() {
				reader.Reset(source)
				content, err := parser.Read(reader, parser.Limits{})
				if err != nil || content == nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLines(b *testing.B) {
	for _, name := range []string{"small", "short_lines", "long_line"} {
		b.Run(name, func(b *testing.B) {
			content, err := parser.New(benchmarkSource(name), parser.Limits{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				total := 0
				for _, line := range content.Lines() {
					total += len(line.Text)
				}
				benchmarkTotal = total
			}
		})
	}
}

func BenchmarkAll(b *testing.B) {
	for _, name := range []string{"small", "short_lines", "long_line"} {
		b.Run(name, func(b *testing.B) {
			content, err := parser.New(benchmarkSource(name), parser.Limits{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				total := 0
				for line := range content.All() {
					total += len(line.Text)
				}
				benchmarkTotal = total
			}
		})
	}
}
