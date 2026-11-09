package cli

import (
	"context"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

// newAPIClient builds the Play API client from global flags.
func newAPIClient(ctx context.Context, a *App, g *globalFlags) (play.API, error) {
	if a.NewAPI == nil {
		return nil, exitcode.Errorf(exitcode.Internal, "API client is not configured")
	}

	return a.NewAPI(ctx, play.Options{
		CredentialsFile: g.Credentials,
		Endpoint:        g.Endpoint,
		UserAgent:       "google-play-publisher/" + a.Version.Version,
		Logger:          a.Logger,
		Debug:           g.Debug,
	})
}
