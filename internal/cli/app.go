package cli

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/vyachin/google-play-publisher/internal/play"
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
	// NewAPI creates the Play API client. It is injected by main and replaced
	// with a fake in tests.
	NewAPI func(ctx context.Context, opts play.Options) (play.API, error)
}
