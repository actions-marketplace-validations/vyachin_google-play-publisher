package exitcode_test

import (
	"errors"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func TestFrom(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want exitcode.Code
	}{
		{name: "nil", err: nil, want: exitcode.OK},
		{name: "plain error", err: errors.New("boom"), want: exitcode.Internal},
		{name: "coded error", err: exitcode.Errorf(exitcode.Rejected, "bad bundle"), want: exitcode.Rejected},
		{name: "wrapped coded error", err: errors.Join(errors.New("context"), exitcode.Wrap(exitcode.Auth, errors.New("denied"))), want: exitcode.Auth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitcode.From(tt.err); got != tt.want {
				t.Fatalf("From() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWrapNil(t *testing.T) {
	if err := exitcode.Wrap(exitcode.Usage, nil); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
}

func TestErrorUnwrap(t *testing.T) {
	base := errors.New("base")
	wrapped := exitcode.Wrap(exitcode.Conflict, base)

	if !errors.Is(wrapped, base) {
		t.Fatal("wrapped error does not expose the base error")
	}
	if got := wrapped.Error(); got != "base" {
		t.Fatalf("Error() = %q, want %q", got, "base")
	}
}

func TestWithHint(t *testing.T) {
	coded := exitcode.Errorf(exitcode.NotFound, "app not found")
	withHint := exitcode.WithHint(coded, "check the package name")

	var e *exitcode.Error
	if !errors.As(withHint, &e) {
		t.Fatal("hint error is not coded")
	}
	if e.Hint != "check the package name" {
		t.Fatalf("Hint = %q", e.Hint)
	}
	if e.Code != exitcode.NotFound {
		t.Fatalf("Code = %d, want %d", e.Code, exitcode.NotFound)
	}

	// The original error must stay unchanged.
	var orig *exitcode.Error
	if !errors.As(coded, &orig) || orig.Hint != "" {
		t.Fatal("WithHint mutated the original error")
	}
}

func TestWithHintPlainError(t *testing.T) {
	withHint := exitcode.WithHint(errors.New("boom"), "try again")
	if got := exitcode.From(withHint); got != exitcode.Internal {
		t.Fatalf("From() = %d, want %d", got, exitcode.Internal)
	}
}
