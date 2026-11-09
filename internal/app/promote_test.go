package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

// trackSet is a fake API whose in-edit track reads come from a map.
func trackSet(tracks map[string]play.TrackState) *playtest.Fake {
	return &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		if state, ok := tracks[track]; ok {
			state.Track = track
			return state, nil
		}
		return play.TrackState{Track: track}, nil
	}}
}

func internalTrack() play.TrackState {
	return play.TrackState{Releases: []play.Release{
		{VersionCodes: []int64{41}, Status: play.StatusCompleted, Name: "1.0.41"},
		{
			VersionCodes: []int64{42},
			Status:       play.StatusCompleted,
			Name:         "1.0.42",
			ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "Internal fixes"}},
		},
	}}
}

func TestPromoteLatestRelease(t *testing.T) {
	fake := trackSet(map[string]play.TrackState{"internal": internalTrack()})

	result, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package: "com.example.app",
		From:    "internal",
		To:      "beta",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	if len(fake.UpdatedTracks) != 1 || fake.UpdatedTracks[0] != "beta" {
		t.Fatalf("updated tracks = %v, want [beta]", fake.UpdatedTracks)
	}
	updated := fake.UpdatedReleases[0]
	if len(updated.VersionCodes) != 1 || updated.VersionCodes[0] != 42 {
		t.Errorf("version codes = %v, want [42]", updated.VersionCodes)
	}
	if updated.Status != play.StatusCompleted {
		t.Errorf("status = %q, want completed", updated.Status)
	}
	if updated.Name != "1.0.42" {
		t.Errorf("name = %q, want the source release name", updated.Name)
	}
	if len(updated.ReleaseNotes) != 1 || updated.ReleaseNotes[0].Text != "Internal fixes" {
		t.Errorf("release notes = %+v, want the source notes", updated.ReleaseNotes)
	}
	if result.Review != app.ReviewSent || result.To != "beta" || result.From != "internal" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestPromoteExplicitVersion(t *testing.T) {
	fake := trackSet(map[string]play.TrackState{"internal": internalTrack()})

	_, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package:     "com.example.app",
		From:        "internal",
		To:          "beta",
		VersionCode: 41,
		ReleaseName: "release-1.0.41",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	updated := fake.UpdatedReleases[0]
	if len(updated.VersionCodes) != 1 || updated.VersionCodes[0] != 41 {
		t.Errorf("version codes = %v, want [41]", updated.VersionCodes)
	}
	if updated.Name != "release-1.0.41" {
		t.Errorf("name = %q, want the explicit release name", updated.Name)
	}
}

func TestPromoteVersionNotOnSource(t *testing.T) {
	fake := trackSet(map[string]play.TrackState{"internal": internalTrack()})

	_, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package:     "com.example.app",
		From:        "internal",
		To:          "beta",
		VersionCode: 99,
	})
	if code := exitcode.From(err); code != exitcode.NotFound {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.NotFound, err)
	}
	if len(fake.UpdatedReleases) != 0 {
		t.Fatalf("updated releases = %d, want 0", len(fake.UpdatedReleases))
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("edit was not cleaned up: %v", fake.DeletedEditIDs)
	}
}

func TestPromoteSkipsDrafts(t *testing.T) {
	source := play.TrackState{Releases: []play.Release{
		{VersionCodes: []int64{42}, Status: play.StatusCompleted, Name: "1.0.42"},
		{VersionCodes: []int64{43}, Status: play.StatusDraft, Name: "1.0.43"},
	}}
	fake := trackSet(map[string]play.TrackState{"internal": source})

	_, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package: "com.example.app",
		From:    "internal",
		To:      "beta",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	updated := fake.UpdatedReleases[0]
	if len(updated.VersionCodes) != 1 || updated.VersionCodes[0] != 42 {
		t.Errorf("version codes = %v, want the non-draft [42]", updated.VersionCodes)
	}
}

func TestPromoteAlreadyOnDestination(t *testing.T) {
	tracks := map[string]play.TrackState{
		"internal": internalTrack(),
		"beta": {Releases: []play.Release{
			{VersionCodes: []int64{42}, Status: play.StatusCompleted},
		}},
	}
	fake := trackSet(tracks)

	_, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package: "com.example.app",
		From:    "internal",
		To:      "beta",
	})
	if code := exitcode.From(err); code != exitcode.Conflict {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Conflict, err)
	}
	if len(fake.UpdatedReleases) != 0 {
		t.Fatalf("updated releases = %d, want 0", len(fake.UpdatedReleases))
	}
}

func TestPromoteStagedProductionRollout(t *testing.T) {
	changelogDir := writeChangelog(t, 42, "en-US", "Production fixes")

	fake := trackSet(map[string]play.TrackState{"beta": internalTrack()})

	result, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package:     "com.example.app",
		From:        "beta",
		To:          "production",
		VersionCode: 42,
		Rollout:     fraction(0.1),
		MetadataDir: changelogDir,
		Confirm:     true,
		Countries:   []string{"fr", "de"},
		ReleaseName: "1.0.42",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	updated := fake.UpdatedReleases[0]
	if updated.Status != play.StatusInProgress || updated.UserFraction != 0.1 {
		t.Errorf("release = %+v, want inProgress at 0.1", updated)
	}
	if len(updated.ReleaseNotes) != 1 || updated.ReleaseNotes[0].Language != "en-US" {
		t.Errorf("release notes = %+v, want the en-US changelog", updated.ReleaseNotes)
	}
	if updated.CountryTargeting == nil || len(updated.CountryTargeting.Countries) != 2 ||
		updated.CountryTargeting.Countries[0] != "DE" || updated.CountryTargeting.Countries[1] != "FR" {
		t.Errorf("country targeting = %+v, want sorted [DE FR]", updated.CountryTargeting)
	}
	if result.Status != "inProgress" {
		t.Errorf("result status = %q, want inProgress", result.Status)
	}
}

func TestPromoteAppliesMetadata(t *testing.T) {
	fake := trackSet(map[string]play.TrackState{"beta": internalTrack()})
	dir := writeListingDir(t)

	result, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package:     "com.example.app",
		From:        "beta",
		To:          "production",
		MetadataDir: dir,
		Confirm:     true,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	if result.Metadata == nil || result.Metadata.Listings != 1 || result.Metadata.Testers != 1 {
		t.Fatalf("metadata result = %+v", result.Metadata)
	}
	if len(fake.Listings) != 1 || fake.Listings[0].Language != "en-US" {
		t.Fatalf("listings = %+v", fake.Listings)
	}
	if len(fake.TesterUpdates) != 1 || fake.TesterUpdates[0].Track != "internal" {
		t.Fatalf("tester updates = %+v", fake.TesterUpdates)
	}
}

func TestPromoteProductionNeedsConfirmation(t *testing.T) {
	fake := &playtest.Fake{}

	_, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package: "com.example.app",
		From:    "beta",
		To:      "production",
	})
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error does not ask for --yes: %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called without confirmation: %v", fake.Calls)
	}
}

func TestPreparePromoteRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		opts app.PromoteOptions
		want string
	}{
		{
			name: "missing from",
			opts: app.PromoteOptions{Package: "com.example.app", To: "beta"},
			want: "--from is required",
		},
		{
			name: "missing to",
			opts: app.PromoteOptions{Package: "com.example.app", From: "internal"},
			want: "--to is required",
		},
		{
			name: "same track",
			opts: app.PromoteOptions{Package: "com.example.app", From: "internal", To: "internal"},
			want: "different tracks",
		},
		{
			name: "empty metadata directory without version code",
			opts: app.PromoteOptions{
				Package: "com.example.app", From: "internal", To: "beta", MetadataDir: t.TempDir(),
			},
			want: "no store metadata found",
		},
		{
			name: "dry run with a saved change",
			opts: app.PromoteOptions{
				Package: "com.example.app", From: "internal", To: "beta",
				DryRun: true, ChangesNotSentForReview: true,
			},
			want: "cannot be combined with --dry-run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &playtest.Fake{}
			_, err := app.NewPublisher(fake, nil).Promote(context.Background(), tt.opts)
			if code := exitcode.From(err); code != exitcode.Usage {
				t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
			if len(fake.Calls) != 0 {
				t.Fatalf("API was called for invalid input: %v", fake.Calls)
			}
		})
	}
}

func TestPromoteDryRunDeletesEdit(t *testing.T) {
	fake := trackSet(map[string]play.TrackState{"internal": internalTrack()})

	result, err := app.NewPublisher(fake, nil).Promote(context.Background(), app.PromoteOptions{
		Package: "com.example.app",
		From:    "internal",
		To:      "beta",
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if result.Review != app.ReviewValidated {
		t.Errorf("review = %q, want validated", result.Review)
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("deleted edits = %v, want one", fake.DeletedEditIDs)
	}
	if len(fake.CommitOptions) != 0 {
		t.Fatalf("dry run committed: %v", fake.CommitOptions)
	}
}

// writeChangelog creates <dir>/<locale>/changelogs/<versionCode>.txt.
func writeChangelog(t *testing.T, versionCode int64, locale, text string) string {
	t.Helper()

	dir := t.TempDir()
	changelogs := filepath.Join(dir, locale, "changelogs")
	if err := os.MkdirAll(changelogs, 0o755); err != nil {
		t.Fatalf("mkdir changelogs: %v", err)
	}
	name := filepath.Join(changelogs, strconv.FormatInt(versionCode, 10)+".txt")
	if err := os.WriteFile(name, []byte(text), 0o600); err != nil {
		t.Fatalf("write changelog: %v", err)
	}
	return dir
}
