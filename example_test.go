package parser_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gatework/parser"
)

func ExampleNew() {
	content, err := parser.New("  alpha\r\nbeta", parser.Limits{})
	if err != nil {
		fmt.Println(err)
		return
	}
	for line := range content.All() {
		fmt.Printf("%d: text=%q raw=%q\n", line.Number, line.Text, line.Raw)
	}
	// Output:
	// 1: text="  alpha" raw="  alpha\r\n"
	// 2: text="beta" raw="beta"
}

func ExampleRead() {
	reader := strings.NewReader("one\ntwo\n")
	content, err := parser.Read(reader, parser.Limits{MaxBytes: 1024, MaxLines: 10})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(content.Len())
	// Output: 2
}

func ExampleCursor() {
	content, err := parser.New("rule allow\r\nobject host\nunknown option\n", parser.Limits{})
	if err != nil {
		fmt.Println(err)
		return
	}
	cursor := content.Cursor()
	// Discover objects first, then process rules with those objects available.
	for _, prefix := range []string{"object ", "rule "} {
		cursor.Rewind()
		for line, ok := cursor.NextUnparsed(); ok; line, ok = cursor.NextUnparsed() {
			if !strings.HasPrefix(line.Text, prefix) {
				continue // Leave this line available to subsequent passes.
			}
			fmt.Println(line.Text)
			if err := cursor.SetParsed(line.Number, true); err != nil {
				fmt.Println(err)
				return
			}
		}
	}
	fmt.Printf("remaining: %q\n", cursor.UnparsedText())
	// Output:
	// object host
	// rule allow
	// remaining: "unknown option\n"
}

func ExampleReadWithContext() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := parser.ReadWithContext(ctx, strings.NewReader("first\nnext"), parser.Limits{MaxLines: 1})
	if limit, ok := errors.AsType[*parser.LimitError](err); ok {
		fmt.Printf("%s: limit %d, observed %d\n", limit.Resource, limit.Limit, limit.Observed)
	}
	// Output: lines: limit 1, observed 2
}

func ExampleContent_LineAt() {
	content, err := parser.New("allow\r\ndeny", parser.Limits{})
	if err != nil {
		fmt.Println(err)
		return
	}
	// The LF at byte offset 6 belongs to the first line.
	if line, ok := content.LineAt(6); ok {
		fmt.Printf("%d: %q\n", line.Number, line.Text)
	}
	// Output: 1: "allow"
}

func ExampleCursor_SetParsedRange() {
	content, err := parser.New("begin\n  item host\nend\npending\n", parser.Limits{})
	if err != nil {
		fmt.Println(err)
		return
	}
	cursor := content.Cursor()
	cursor.Next()
	// Validate the complete block and save its business result before marking.
	if err := cursor.SetParsedRange(1, 3, true); err != nil {
		fmt.Println(err)
		return
	}
	for line := range cursor.Unparsed() {
		fmt.Printf("%d: %s\n", line.Number, line.Text)
	}
	fmt.Println("position:", cursor.Position())
	// Output:
	// 4: pending
	// position: 1
}

func ExampleCursor_Seek() {
	content, err := parser.New("hostname edge\nbanner motd ^\nwelcome\n^\nntp server 192.0.2.1\n", parser.Limits{})
	if err != nil {
		fmt.Println(err)
		return
	}
	// Locate a construct in the whole source, then continue line by line there.
	line, ok := content.LineAt(strings.Index(content.Text(), "banner"))
	if !ok {
		return
	}
	cursor := content.Cursor()
	if err := cursor.Seek(line.Number - 1); err != nil {
		fmt.Println(err)
		return
	}
	next, _ := cursor.Next()
	fmt.Printf("%d: %s\n", next.Number, next.Text)
	// Output: 2: banner motd ^
}

func ExampleContent_Span() {
	content, err := parser.New("hostname edge\nbanner motd ^\nwelcome\r\n^\nntp server 192.0.2.1\n", parser.Limits{})
	if err != nil {
		fmt.Println(err)
		return
	}
	cursor := content.Cursor()
	// Keep a multi-line block byte for byte, then mark it parsed as one unit.
	if block, ok := content.Span(2, 4); ok {
		fmt.Printf("%q\n", block)
	}
	if err := cursor.SetParsedRange(2, 4, true); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("unparsed:", cursor.UnparsedCount())
	// Output:
	// "banner motd ^\nwelcome\r\n^\n"
	// unparsed: 2
}
