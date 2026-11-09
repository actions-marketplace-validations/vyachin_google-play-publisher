// Package exitcode defines stable process exit codes and errors that carry them.
package exitcode

import (
	"errors"
	"fmt"
)

// Code is a process exit code.
type Code int

// Stable exit codes. See docs/03-cli-contract.md.
const (
	OK           Code = 0
	Internal     Code = 1
	Usage        Code = 2
	Auth         Code = 3
	NotFound     Code = 4
	Conflict     Code = 5
	Transient    Code = 6
	Rejected     Code = 7
	ManualAction Code = 8
	Interrupted  Code = 130
)

// Error carries an exit code and an optional hint for the user.
type Error struct {
	Code Code
	Err  error
	Hint string
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying error.
func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an error with the given exit code.
func Errorf(code Code, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// Wrap attaches code to err. It returns nil when err is nil.
func Wrap(code Code, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}

// WithHint returns a copy of a coded error with the given hint. Plain errors
// are wrapped as Internal.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}

	var coded *Error
	if errors.As(err, &coded) {
		withHint := *coded
		withHint.Hint = hint
		return &withHint
	}
	return &Error{Code: Internal, Err: err, Hint: hint}
}

// From returns the code carried by err. Untyped errors map to Internal.
func From(err error) Code {
	if err == nil {
		return OK
	}

	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return Internal
}
