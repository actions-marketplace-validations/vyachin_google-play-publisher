package cli

import (
	"io"
	"log/slog"
	"time"

	"github.com/vyachin/google-play-publisher/internal/version"
)

// App holds dependencies shared by all commands. It is created once in main
// and passed to the command tree, so tests can replace writers and the clock.
type App struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Now     func() time.Time
	Logger  *slog.Logger
	Version version.Info
}
