package app

import (
	"context"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

// StatusResult is a read-only view of the releases on a track.
type StatusResult struct {
	Package  string
	Track    string
	Releases []play.ReleaseSummary
}

// Status lists releases on a track without creating an edit.
func Status(ctx context.Context, api play.API, pkg, track string) (StatusResult, error) {
	if pkg == "" {
		return StatusResult{}, exitcode.Errorf(exitcode.Usage, "--package is required")
	}
	if track == "" {
		track = "internal"
	}

	releases, err := api.TrackReleases(ctx, pkg, track)
	if err != nil {
		return StatusResult{}, err
	}
	if releases == nil {
		releases = []play.ReleaseSummary{}
	}

	return StatusResult{Package: pkg, Track: track, Releases: releases}, nil
}
