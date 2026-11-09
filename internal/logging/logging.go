// Package logging configures the process logger.
package logging

import (
	"io"
	"log/slog"
)

// Options controls logger construction.
type Options struct {
	Debug  bool
	Format string
	Out    io.Writer
}

// New returns a logger writing to opts.Out. Debug enables the DEBUG level and
// source locations.
func New(opts Options) *slog.Logger {
	level := slog.LevelInfo
	if opts.Debug {
		level = slog.LevelDebug
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.Debug,
	}

	var handler slog.Handler
	if opts.Format == "json" {
		handler = slog.NewJSONHandler(opts.Out, handlerOpts)
	} else {
		handler = slog.NewTextHandler(opts.Out, handlerOpts)
	}
	return slog.New(handler)
}

// ValidFormat reports whether f is a supported log format.
func ValidFormat(f string) bool {
	return f == "text" || f == "json"
}
