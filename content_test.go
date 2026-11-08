package parser_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"testing/synctest"

	"github.com/gatework/parser"
)

func newContent(t *testing.T, text string) *parser.Content {
	t.Helper()
	content, err := parser.New(text, parser.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		input string
		raw   []string
		text  []string
	}{
		{name: "empty", raw: []string{}, text: []string{}},
		{name: "unterminated", input: "word", raw: []string{"word"}, text: []string{"word"}},
		{name: "lf", input: "a\nb\n", raw: []string{"a\n", "b\n"}, text: []string{"a", "b"}},
		{name: "crlf", input: "a\r\nb\r\n", raw: []string{"a\r\n", "b\r\n"}, text: []string{"a", "b"}},
		{name: "cr", input: "a\rb\r", raw: []string{"a\r", "b\r"}, text: []string{"a", "b"}},
		{
			name: "mixed endings", input: "a\r\nb\rc\nd",
			raw: []string{"a\r\n", "b\r", "c\n", "d"}, text: []string{"a", "b", "c", "d"},
		},
		{name: "empty lines", input: "\r\n\n\r", raw: []string{"\r\n", "\n", "\r"}, text: []string{"", "", ""}},
		{name: "consecutive cr", input: "a\r\r\n", raw: []string{"a\r", "\r\n"}, text: []string{"a", ""}},
		{name: "indentation", input: " \t a \t\n", raw: []string{" \t a \t\n"}, text: []string{" \t a \t"}},
		{
			name: "unicode and bom", input: "\ufeff中文\r\n🙂\n",
			raw: []string{"\ufeff中文\r\n", "🙂\n"}, text: []string{"\ufeff中文", "🙂"},
		},
		{
			name: "arbitrary bytes", input: "\xff\x00\n\xfe",
			raw: []string{"\xff\x00\n", "\xfe"}, text: []string{"\xff\x00", "\xfe"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := newContent(t, tt.input)
			if content.String() != tt.input || content.Digest() != sha256.Sum256([]byte(tt.input)) {
				t.Fatal("source or digest changed")
			}
			if content.Len() != len(tt.raw) || len(content.Lines()) != len(tt.raw) {
				t.Fatalf("line count = %d, want %d", content.Len(), len(tt.raw))
			}
			offset := 0
			for i, line := range content.Lines() {
				want := parser.Line{
					Number: i + 1, StartByte: offset, EndByte: offset + len(tt.raw[i]),
					Raw: tt.raw[i], Text: tt.text[i],
				}
				if line != want {
					t.Fatalf("line = %#v, want %#v", line, want)
				}
				if got, ok := content.Line(i + 1); !ok || got != want {
					t.Fatalf("indexed line = %#v, %v", got, ok)
				}
				offset += len(tt.raw[i])
			}
			for _, number := range []int{-1, 0, content.Len() + 1} {
				if line, ok := content.Line(number); ok || line != (parser.Line{}) {
					t.Fatalf("Line(%d) = %#v, %v", number, line, ok)
				}
			}
		})
	}
}

func TestNewLongLine(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", 128<<10)
	content := newContent(t, text+"\r\nlast")
	line, ok := content.Line(1)
	if !ok || line.Text != text || line.EndByte != len(text)+2 || content.Len() != 2 {
		t.Fatal("long physical line was truncated or split")
	}
}

func TestInputLimits(t *testing.T) {
	constructors := []struct {
		name string
		new  func(string, parser.Limits) (*parser.Content, error)
	}{
		{name: "string", new: parser.New},
		{name: "reader", new: func(text string, limits parser.Limits) (*parser.Content, error) {
			return parser.Read(strings.NewReader(text), limits)
		}},
	}
	tests := []struct {
		name   string
		input  string
		limits parser.Limits
		err    error
	}{
		{name: "exact bytes", input: "ab", limits: parser.Limits{MaxBytes: 2}},
		{name: "excess bytes", input: "abc", limits: parser.Limits{MaxBytes: 2}, err: parser.ErrLimit},
		{name: "byte units", input: "中", limits: parser.Limits{MaxBytes: 2}, err: parser.ErrLimit},
		{name: "exact terminated lines", input: "a\nb\n", limits: parser.Limits{MaxLines: 2}},
		{name: "exact unterminated lines", input: "a\nb", limits: parser.Limits{MaxLines: 2}},
		{name: "excess terminated lines", input: "a\nb\n", limits: parser.Limits{MaxLines: 1}, err: parser.ErrLimit},
		{name: "excess unterminated lines", input: "a\nb", limits: parser.Limits{MaxLines: 1}, err: parser.ErrLimit},
		{name: "empty lines count", input: "\n\n", limits: parser.Limits{MaxLines: 1}, err: parser.ErrLimit},
		{name: "negative bytes", limits: parser.Limits{MaxBytes: -1}, err: parser.ErrInvalidLimits},
		{name: "negative lines", limits: parser.Limits{MaxLines: -1}, err: parser.ErrInvalidLimits},
		{name: "maximum integer", input: "x", limits: parser.Limits{MaxBytes: int(^uint(0) >> 1)}},
	}
	for _, constructor := range constructors {
		t.Run(constructor.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					content, err := constructor.new(tt.input, tt.limits)
					if !errors.Is(err, tt.err) {
						t.Fatalf("error = %v, want %v", err, tt.err)
					}
					if tt.err != nil {
						if content != nil {
							t.Fatal("failed construction exposed partial content")
						}
						return
					}
					if content == nil || content.String() != tt.input {
						t.Fatal("accepted input changed")
					}
				})
			}
		})
	}
}

func TestDefaultLimits(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "bytes", input: strings.Repeat("x", parser.DefaultMaxBytes+1)},
		{name: "lines", input: strings.Repeat("\n", parser.DefaultMaxLines+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if content, err := parser.New(tt.input, parser.Limits{}); content != nil || !errors.Is(err, parser.ErrLimit) {
				t.Fatalf("default limit: content present = %v, error = %v", content != nil, err)
			}
			content, err := parser.Read(strings.NewReader(tt.input), parser.Limits{})
			if content != nil || !errors.Is(err, parser.ErrLimit) {
				t.Fatalf("default reader limit: content present = %v, error = %v", content != nil, err)
			}
		})
	}
}

func TestRead(t *testing.T) {
	const text = " \t中文\r\n\xff\nlast\r"
	tests := []struct {
		name string
		wrap func(io.Reader) io.Reader
	}{
		{name: "single bytes", wrap: iotest.OneByteReader},
		{name: "short reads", wrap: iotest.HalfReader},
		{name: "data with eof", wrap: iotest.DataErrReader},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := parser.Read(tt.wrap(strings.NewReader(text)), parser.Limits{MaxBytes: len(text)})
			if err != nil {
				t.Fatal(err)
			}
			if content.String() != text || content.Len() != 3 {
				t.Fatal("chunked reads changed the content")
			}
		})
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

func TestReadFailures(t *testing.T) {
	errSource := errors.New("source failed")
	tests := []struct {
		name string
		r    io.Reader
		err  error
	}{
		{name: "nil", err: parser.ErrInvalidReader},
		{name: "immediate failure", r: iotest.ErrReader(errSource), err: errSource},
		{name: "data with failure", r: readFunc(func(p []byte) (int, error) {
			return copy(p, "partial"), errSource
		}), err: errSource},
		{name: "truncated input", r: readFunc(func(p []byte) (int, error) {
			return copy(p, "partial"), io.ErrUnexpectedEOF
		}), err: io.ErrUnexpectedEOF},
		{name: "negative read count", r: readFunc(func([]byte) (int, error) {
			return -1, nil
		}), err: parser.ErrInvalidReader},
		{name: "oversized read count", r: readFunc(func(p []byte) (int, error) {
			return len(p) + 1, nil
		}), err: parser.ErrInvalidReader},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := parser.Read(tt.r, parser.Limits{})
			if content != nil || !errors.Is(err, tt.err) {
				t.Fatalf("content present = %v, error = %v, want %v", content != nil, err, tt.err)
			}
		})
	}
}

func TestReadProgress(t *testing.T) {
	t.Run("temporary empty reads", func(t *testing.T) {
		calls := 0
		r := readFunc(func(p []byte) (int, error) {
			calls++
			if calls <= 3 {
				return 0, nil
			}
			return copy(p, "ok"), io.EOF
		})
		content, err := parser.Read(r, parser.Limits{})
		if err != nil || content.String() != "ok" {
			t.Fatalf("temporary empty read: %v", err)
		}
	})
	t.Run("no progress", func(t *testing.T) {
		calls := 0
		r := readFunc(func([]byte) (int, error) {
			calls++
			if calls > 100 {
				t.Fatal("reader kept retrying without progress")
			}
			return 0, nil
		})
		if content, err := parser.Read(r, parser.Limits{}); content != nil || !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("no progress: content present = %v, error = %v", content != nil, err)
		}
	})
}

type trackedReader struct {
	reader   io.Reader
	bytes    int
	isClosed bool
}

func (r *trackedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}

func (r *trackedReader) Close() error {
	r.isClosed = true
	return nil
}

func TestReadOwnershipAndBound(t *testing.T) {
	for _, input := range []string{"abc", "abcdefgh"} {
		t.Run(input, func(t *testing.T) {
			r := &trackedReader{reader: strings.NewReader(input)}
			content, err := parser.Read(r, parser.Limits{MaxBytes: 3})
			if len(input) == 3 {
				if err != nil || content.String() != input {
					t.Fatalf("exact limit: %v", err)
				}
			} else if content != nil || !errors.Is(err, parser.ErrLimit) {
				t.Fatalf("oversize error = %v", err)
			}
			if r.bytes != min(len(input), 4) || r.isClosed {
				t.Fatalf("consumed %d bytes, closed = %v", r.bytes, r.isClosed)
			}
		})
	}
}

func TestContentZeroValueAndIsolation(t *testing.T) {
	var empty parser.Content
	if empty.String() != "" || empty.Len() != 0 || empty.Digest() != sha256.Sum256(nil) {
		t.Fatal("zero Content is not empty")
	}
	if line, ok := empty.Cursor().Next(); ok || line != (parser.Line{}) {
		t.Fatal("empty Content returned a line")
	}
	content := newContent(t, "first\r\nsecond\n")
	lines := content.Lines()
	lines[0] = parser.Line{Text: "changed"}
	line, _ := content.Line(1)
	if line.Raw != "first\r\n" || line.Number != 1 {
		t.Fatal("returned slice changed the content")
	}
	digest := content.Digest()
	digest[0] ^= 1
	if content.Digest() == digest {
		t.Fatal("returned digest changed the content")
	}
}

func TestIncrementalLineLimits(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		limit  int
		want   string
		failAt int
	}{
		{name: "split crlf at limit", chunks: []string{"a\r", "\n"}, limit: 1, want: "a\r\n"},
		{name: "next line after split crlf", chunks: []string{"a\r", "\nb"}, limit: 1, failAt: 2},
		{name: "next line after cr", chunks: []string{"a\r", "b"}, limit: 1, failAt: 2},
		{name: "next line after lf", chunks: []string{"a\nb"}, limit: 1, failAt: 1},
		{name: "extra empty line", chunks: []string{"a\n", "\n"}, limit: 1, failAt: 2},
		{name: "empty crlf line", chunks: []string{"\r", "\n"}, limit: 1, want: "\r\n"},
		{name: "consecutive cr", chunks: []string{"a\r", "\r", "\n"}, limit: 2, want: "a\r\r\n"},
		{name: "unterminated", chunks: []string{"a\r", "\n", "b"}, limit: 2, want: "a\r\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			r := readFunc(func(p []byte) (int, error) {
				calls++
				if tt.failAt > 0 && calls > tt.failAt {
					t.Fatal("read continued after the first excess line was observed")
				}
				if calls > len(tt.chunks) {
					return 0, io.EOF
				}
				return copy(p, tt.chunks[calls-1]), nil
			})
			content, err := parser.Read(r, parser.Limits{MaxLines: tt.limit})
			if tt.failAt > 0 {
				limitErr, ok := errors.AsType[*parser.LimitError](err)
				if content != nil || !ok || !errors.Is(err, parser.ErrLimit) {
					t.Fatalf("expected a structured line limit error, got %v", err)
				}
				if limitErr.Resource != "lines" || limitErr.Limit != tt.limit || limitErr.Observed != tt.limit+1 {
					t.Fatalf("wrong observed limit: %+v", limitErr)
				}
				return
			}
			if err != nil || content.String() != tt.want {
				t.Fatalf("incremental read failed: %v", err)
			}
			want := newContent(t, tt.want)
			if !slices.Equal(content.Lines(), want.Lines()) || content.Digest() != want.Digest() {
				t.Fatal("chunk boundaries changed the physical lines or digest")
			}
		})
	}
}

func TestLimitAndReadErrors(t *testing.T) {
	errSource := errors.New("source failed")
	for _, resource := range []string{"bytes", "lines"} {
		t.Run(resource, func(t *testing.T) {
			calls := 0
			limits := parser.Limits{MaxBytes: 1}
			if resource == "lines" {
				limits = parser.Limits{MaxLines: 1}
			}
			r := readFunc(func(p []byte) (int, error) {
				calls++
				if resource == "lines" {
					return copy(p, "a\nb"), errSource
				}
				if calls == 1 {
					return copy(p, "a"), nil
				}
				return copy(p, "b"), errSource
			})
			content, err := parser.Read(r, limits)
			if content != nil || !errors.Is(err, errSource) || !errors.Is(err, parser.ErrLimit) {
				t.Fatalf("combined failure lost a cause: %v", err)
			}
			limitErr, ok := errors.AsType[*parser.LimitError](err)
			if !ok || limitErr.Resource != resource || limitErr.Limit != 1 || limitErr.Observed != 2 {
				t.Fatalf("wrong observed limit: %+v", limitErr)
			}
		})
	}
	if content, err := parser.New("abcd", parser.Limits{MaxBytes: 2}); content != nil {
		t.Fatal("oversized input returned content")
	} else if limitErr, ok := errors.AsType[*parser.LimitError](err); !ok || limitErr.Observed != 4 {
		t.Fatalf("known source size was not reported: %v", err)
	}
}

func TestReadWrappedEOF(t *testing.T) {
	errSource := errors.New("source failed")
	for _, readErr := range []error{fmt.Errorf("wrapped: %w", io.EOF), errors.Join(io.EOF, errSource)} {
		r := readFunc(func(p []byte) (int, error) { return copy(p, "partial"), readErr })
		content, err := parser.Read(r, parser.Limits{})
		if content != nil || !errors.Is(err, readErr) {
			t.Fatalf("non-graceful eof accepted: %v", err)
		}
	}
}

// Cancel deterministically after enough cooperative checks to cross a chunk
// boundary. This exercises CPU cancellation without timing a goroutine race.
type cancelOnCheck struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *cancelOnCheck) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestConstructionCancellation(t *testing.T) {
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r := readFunc(func([]byte) (int, error) {
			t.Fatal("pre-canceled operation read input")
			return 0, io.EOF
		})
		if content, err := parser.ReadWithContext(ctx, r, parser.Limits{}); content != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-canceled read: %v", err)
		}
		if content, err := parser.NewWithContext(ctx, "source", parser.Limits{}); content != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-canceled construction: %v", err)
		}
	})
	for _, name := range []string{"between chunks", "before publication"} {
		t.Run(name, func(t *testing.T) {
			base, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := &cancelOnCheck{Context: base, cancel: cancel, remaining: 3}
			source := "short source"
			if name == "between chunks" {
				source = strings.Repeat("x", 256<<10)
			}
			content, err := parser.NewWithContext(ctx, source, parser.Limits{})
			if content != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled source was published: %v", err)
			}
		})
	}
	t.Run("final read cancels", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		r := readFunc(func(p []byte) (int, error) {
			cancel()
			return copy(p, "last"), io.EOF
		})
		content, err := parser.ReadWithContext(ctx, r, parser.Limits{})
		if content != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("final-read cancellation lost: %v", err)
		}
	})
	t.Run("cancel limit and read error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errSource := errors.New("source failed")
		calls := 0
		r := readFunc(func(p []byte) (int, error) {
			calls++
			if calls == 1 {
				return copy(p, "a"), nil
			}
			cancel()
			return copy(p, "b"), errSource
		})
		content, err := parser.ReadWithContext(ctx, r, parser.Limits{MaxBytes: 1})
		if content != nil {
			t.Fatal("failed read exposed content")
		}
		for _, cause := range []error{context.Canceled, parser.ErrLimit, errSource} {
			if !errors.Is(err, cause) {
				t.Fatalf("combined failure lost %v: %v", cause, err)
			}
		}
	})
}

func TestReadCancellationKeepsReaderOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		started := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		r := &trackedReader{reader: readFunc(func(p []byte) (int, error) {
			close(started)
			<-release
			return copy(p, "last"), io.EOF
		})}
		go func() {
			content, err := parser.ReadWithContext(ctx, r, parser.Limits{})
			if content != nil {
				err = errors.New("published canceled content")
			}
			done <- err
		}()
		<-started
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			t.Errorf("returned while the borrowed reader was still blocked: %v", err)
			close(release)
			return
		default:
		}
		close(release)
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation not returned after read completed: %v", err)
		}
		if r.isClosed {
			t.Fatal("library closed its borrowed reader")
		}
	})
}

func TestContentLineAtAndAll(t *testing.T) {
	content := newContent(t, "中\r\n\nx\rz")
	for offset, number := range []int{1, 1, 1, 1, 1, 2, 3, 3, 4} {
		line, ok := content.LineAt(offset)
		if !ok || line.Number != number {
			t.Fatalf("LineAt(%d) = %#v, %v", offset, line, ok)
		}
	}
	for _, offset := range []int{-1, 9, 10} {
		if line, ok := content.LineAt(offset); ok || line != (parser.Line{}) {
			t.Fatalf("invalid offset accepted: %d", offset)
		}
	}
	all := content.All()
	for range 2 {
		if !slices.Equal(slices.Collect(all), content.Lines()) {
			t.Fatal("iterator is not a repeatable source-order traversal")
		}
		count := 0
		for range all {
			count++
			break
		}
		if count != 1 {
			t.Fatal("early stop was ignored")
		}
	}
	var empty parser.Content
	if _, ok := empty.LineAt(0); ok || len(slices.Collect(empty.All())) != 0 {
		t.Fatal("empty content produced a line")
	}
}
