// Package logging configures the process logger.
package logging

import (
	"io"
	"log/slog"
)

// Options controls logger construction.
type Options struct {
	Debug bool
	Out   io.Writer
}

// New returns a text logger writing to opts.Out. Debug enables the DEBUG
// level and source locations.
func New(opts Options) *slog.Logger {
	level := slog.LevelInfo
	if opts.Debug {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(opts.Out, &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.Debug,
	}))
}
