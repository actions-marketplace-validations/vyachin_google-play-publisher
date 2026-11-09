package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func fraction(value float64) *float64 { return &value }

// productionTrack returns a track with a completed release and a staged one.
func productionTrack(track string) play.TrackState {
	return play.TrackState{Track: track, Releases: []play.Release{
		{
			VersionCodes: []int64{41},
			Status:       play.StatusCompleted,
			Name:         "1.0.41",
		},
		{
			VersionCodes:        []int64{42},
			Status:              play.StatusInProgress,
			UserFraction:        0.1,
			Name:                "1.0.42",
			InAppUpdatePriority: 2,
			ReleaseNotes:        []play.LocalizedText{{Language: "en-US", Text: "Fixes"}},
		},
	}}
}

func TestRolloutIncreasesFraction(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return productionTrack(track), nil
	}}

	publisher := app.NewPublisher(fake, nil)
	result, err := publisher.Rollout(context.Background(), app.RolloutOptions{
		Package:  "com.example.app",
		Track:    "production",
		Fraction: fraction(0.25),
	})
	if err != nil {
		t.Fatalf("Rollout: %v", err)
	}

	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("updated releases = %d, want 1", len(fake.UpdatedReleases))
	}
	updated := fake.UpdatedReleases[0]
	if updated.Status != play.StatusInProgress {
		t.Errorf("status = %q, want inProgress", updated.Status)
	}
	if updated.UserFraction != 0.25 {
		t.Errorf("fraction = %v, want 0.25", updated.UserFraction)
	}
	if len(updated.VersionCodes) != 1 || updated.VersionCodes[0] != 42 {
		t.Errorf("version codes = %v, want [42]", updated.VersionCodes)
	}
	if updated.Name != "1.0.42" || updated.InAppUpdatePriority != 2 {
		t.Errorf("release metadata was not preserved: %+v", updated)
	}
	if len(updated.ReleaseNotes) != 1 || updated.ReleaseNotes[0].Text != "Fixes" {
		t.Errorf("release notes were not preserved: %+v", updated.ReleaseNotes)
	}
	if result.Review != app.ReviewSent || result.Status != "inProgress" || result.UserFraction != 0.25 {
		t.Errorf("unexpected result: %+v", result)
	}
	if result.EditID != "edit-1" {
		t.Errorf("edit id = %q, want edit-1", result.EditID)
	}

	want := []string{"CreateEdit", "TrackEdit", "UpdateTrack", "CommitEdit"}
	if strings.Join(fake.Calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", fake.Calls, want)
	}
}

func TestRolloutHaltsAndKeepsFraction(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return productionTrack(track), nil
	}}

	result, err := app.NewPublisher(fake, nil).Rollout(context.Background(), app.RolloutOptions{
		Package:     "com.example.app",
		Track:       "production",
		VersionCode: 42,
		Halt:        true,
	})
	if err != nil {
		t.Fatalf("Rollout: %v", err)
	}

	updated := fake.UpdatedReleases[0]
	if updated.Status != play.StatusHalted {
		t.Errorf("status = %q, want halted", updated.Status)
	}
	if updated.UserFraction != 0.1 {
		t.Errorf("fraction = %v, want the existing 0.1", updated.UserFraction)
	}
	if result.Status != "halted" {
		t.Errorf("result status = %q, want halted", result.Status)
	}
}

func TestRolloutCompletes(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return productionTrack(track), nil
	}}

	_, err := app.NewPublisher(fake, nil).Rollout(context.Background(), app.RolloutOptions{
		Package:  "com.example.app",
		Track:    "production",
		Complete: true,
	})
	if err != nil {
		t.Fatalf("Rollout: %v", err)
	}

	updated := fake.UpdatedReleases[0]
	if updated.Status != play.StatusCompleted {
		t.Errorf("status = %q, want completed", updated.Status)
	}
	if updated.UserFraction != 0 {
		t.Errorf("fraction = %v, want 0 for a completed release", updated.UserFraction)
	}
}

func TestRolloutRequiresMatchingRelease(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return play.TrackState{Track: track, Releases: []play.Release{
			{VersionCodes: []int64{41}, Status: play.StatusCompleted},
		}}, nil
	}}

	_, err := app.NewPublisher(fake, nil).Rollout(context.Background(), app.RolloutOptions{
		Package:  "com.example.app",
		Track:    "production",
		Fraction: fraction(0.2),
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

func TestRolloutRejectsAmbiguousRelease(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return play.TrackState{Track: track, Releases: []play.Release{
			{VersionCodes: []int64{41}, Status: play.StatusInProgress, UserFraction: 0.1},
			{VersionCodes: []int64{42}, Status: play.StatusInProgress, UserFraction: 0.2},
		}}, nil
	}}

	_, err := app.NewPublisher(fake, nil).Rollout(context.Background(), app.RolloutOptions{
		Package:  "com.example.app",
		Track:    "production",
		Fraction: fraction(0.3),
	})
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "--version-code") {
		t.Fatalf("error does not ask for --version-code: %v", err)
	}
	if len(fake.UpdatedReleases) != 0 {
		t.Fatalf("updated releases = %d, want 0", len(fake.UpdatedReleases))
	}
}

func TestRolloutDryRunDeletesEdit(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return productionTrack(track), nil
	}}

	result, err := app.NewPublisher(fake, nil).Rollout(context.Background(), app.RolloutOptions{
		Package:  "com.example.app",
		Track:    "production",
		Fraction: fraction(0.2),
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Rollout: %v", err)
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

func TestRolloutManualFallbackSavesWithoutReview(t *testing.T) {
	calls := 0
	fake := &playtest.Fake{
		TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
			return productionTrack(track), nil
		},
		CommitEditFunc: func(_ context.Context, _, _ string, _ play.CommitOptions) error {
			calls++
			if calls == 1 {
				return exitcode.Errorf(exitcode.ManualAction, "changes cannot be sent for review automatically")
			}
			return nil
		},
	}

	result, err := app.NewPublisher(fake, nil).Rollout(context.Background(), app.RolloutOptions{
		Package:  "com.example.app",
		Track:    "production",
		Fraction: fraction(0.2),
	})
	if code := exitcode.From(err); code != exitcode.ManualAction {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.ManualAction, err)
	}
	if result.Review != app.ReviewNotSent {
		t.Errorf("review = %q, want notSent", result.Review)
	}
	if result.EditID == "" {
		t.Error("saved result has no edit id")
	}
	if len(fake.CommitOptions) != 2 || !fake.CommitOptions[1].ChangesNotSentForReview {
		t.Errorf("commit options = %+v, want the second call saved without review", fake.CommitOptions)
	}
}

func TestPrepareRolloutRejectsBadInputBeforeAPI(t *testing.T) {
	tests := []struct {
		name string
		opts app.RolloutOptions
		want string
	}{
		{
			name: "no operation",
			opts: app.RolloutOptions{Package: "com.example.app"},
			want: "--fraction, --halt or --complete",
		},
		{
			name: "fraction and halt",
			opts: app.RolloutOptions{Package: "com.example.app", Fraction: fraction(0.2), Halt: true},
			want: "mutually exclusive",
		},
		{
			name: "fraction out of range",
			opts: app.RolloutOptions{Package: "com.example.app", Fraction: fraction(1)},
			want: "between 0 and 1",
		},
		{
			name: "bad package",
			opts: app.RolloutOptions{Package: "com", Fraction: fraction(0.2)},
			want: "invalid package name",
		},
		{
			name: "bad track",
			opts: app.RolloutOptions{Package: "com.example.app", Track: "pro duction", Fraction: fraction(0.2)},
			want: "invalid track name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &playtest.Fake{}
			_, err := app.NewPublisher(fake, nil).Rollout(context.Background(), tt.opts)
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
