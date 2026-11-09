// Command gplay publishes signed Android App Bundles to Google Play.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &cli.App{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Now:     time.Now,
		Version: version.New(),
	}

	os.Exit(cli.Execute(ctx, app, os.Args[1:]))
}
