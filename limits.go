package parser

import (
	"errors"
	"fmt"
)

const (
	// DefaultMaxBytes is the source size limit used by zero-value Limits.
	DefaultMaxBytes = 16 << 20
	// DefaultMaxLines is the physical line limit used by zero-value Limits.
	DefaultMaxLines = 200_000
)

var (
	// ErrInvalidLimits indicates a negative limit.
	ErrInvalidLimits = errors.New("parser: invalid limits")
	// ErrLimit indicates that the source exceeds a configured limit.
	ErrLimit = errors.New("parser: source limit exceeded")
)

// LimitError reports an exceeded source limit. Resource is "bytes" or "lines".
// Observed is the amount observed when the limit was exceeded; it may be a
// lower bound on the total input size because reading stops early.
type LimitError struct {
	Resource string
	Limit    int
	Observed int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("parser: %s limit exceeded: limit %d, observed %d", e.Resource, e.Limit, e.Observed)
}

// Unwrap preserves errors.Is(err, ErrLimit) matching.
func (e *LimitError) Unwrap() error { return ErrLimit }

// Limits bounds the accepted source. Each zero field uses its default;
// negative values are invalid. Positive values override the defaults.
type Limits struct {
	MaxBytes int
	MaxLines int
}

func (l Limits) normalized() (Limits, error) {
	if l.MaxBytes < 0 {
		return Limits{}, fmt.Errorf("%w: max bytes is negative", ErrInvalidLimits)
	}
	if l.MaxLines < 0 {
		return Limits{}, fmt.Errorf("%w: max lines is negative", ErrInvalidLimits)
	}
	if l.MaxBytes == 0 {
		l.MaxBytes = DefaultMaxBytes
	}
	if l.MaxLines == 0 {
		l.MaxLines = DefaultMaxLines
	}
	return l, nil
}
