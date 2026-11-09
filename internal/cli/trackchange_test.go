package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

// stagedTrack returns a production track with one staged release.
func stagedTrack(_ context.Context, _ string, _ string, track string) (play.TrackState, error) {
	return play.TrackState{Track: track, Releases: []play.Release{{
		VersionCodes:        []int64{42},
		Status:              play.StatusInProgress,
		UserFraction:        0.1,
		Name:                "1.0.42",
		ReleaseNotes:        []play.LocalizedText{{Language: "en-US", Text: "Fixes"}},
		InAppUpdatePriority: 1,
	}}}, nil
}

func TestRolloutJSONReportsFraction(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: stagedTrack}

	out, errOut, code := runWithAPI(t, fake, "rollout", "-p", "com.example.app",
		"-t", "production", "--fraction", "0.25", "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}

	got := decodeSingleObject(t, out)
	if got["command"] != "rollout" || got["package"] != "com.example.app" || got["track"] != "production" {
		t.Fatalf("unexpected object: %v", got)
	}
	if got["status"] != "inProgress" || got["userFraction"] != 0.25 {
		t.Fatalf("status/fraction = %v/%v", got["status"], got["userFraction"])
	}
	versions, ok := got["versionCodes"].([]any)
	if !ok || len(versions) != 1 || versions[0] != float64(42) {
		t.Fatalf("versionCodes = %#v, want [42]", got["versionCodes"])
	}
	if got["review"] != "sent" || got["editId"] != "edit-1" {
		t.Fatalf("review/editId = %v/%v", got["review"], got["editId"])
	}

	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("updated releases = %d, want 1", len(fake.UpdatedReleases))
	}
	if fake.UpdatedReleases[0].UserFraction != 0.25 {
		t.Errorf("fraction = %v, want 0.25", fake.UpdatedReleases[0].UserFraction)
	}
}

func TestRolloutTextOutput(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: stagedTrack}

	out, errOut, code := runWithAPI(t, fake, "rollout", "-p", "com.example.app", "-t", "production", "--halt")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}
	if !strings.Contains(out, "halted com.example.app version 42 on production") {
		t.Fatalf("unexpected output: %q", out)
	}
	if fake.UpdatedReleases[0].UserFraction != 0.1 {
		t.Errorf("halt lost the fraction: %v", fake.UpdatedReleases[0].UserFraction)
	}
}

func TestRolloutRejectsBadFlagsBeforeAPI(t *testing.T) {
	fake := &playtest.Fake{}

	_, errOut, code := runWithAPI(t, fake, "rollout", "-p", "com.example.app",
		"--fraction", "0.2", "--halt")
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Fatalf("unexpected error: %s", errOut)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}

func TestRolloutMissingOperation(t *testing.T) {
	fake := &playtest.Fake{}

	_, errOut, code := runWithAPI(t, fake, "rollout", "-p", "com.example.app")
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.Usage, errOut)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}

func TestRolloutMissingReleaseIsNotFound(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: func(_ context.Context, _, _, track string) (play.TrackState, error) {
		return play.TrackState{Track: track}, nil
	}}

	_, errOut, code := runWithAPI(t, fake, "rollout", "-p", "com.example.app",
		"-t", "production", "--fraction", "0.2")
	if code != int(exitcode.NotFound) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.NotFound, errOut)
	}
	if !strings.Contains(errOut, "no inProgress or halted release") {
		t.Fatalf("unexpected error: %s", errOut)
	}
}

func promoteSource(_ context.Context, _ string, _ string, track string) (play.TrackState, error) {
	switch track {
	case "internal":
		return play.TrackState{Track: track, Releases: []play.Release{{
			VersionCodes: []int64{42},
			Status:       play.StatusCompleted,
			Name:         "1.0.42",
		}}}, nil
	default:
		return play.TrackState{Track: track}, nil
	}
}

func TestPromoteJSON(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: promoteSource}

	out, errOut, code := runWithAPI(t, fake, "promote", "-p", "com.example.app",
		"--from", "internal", "--to", "beta", "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}

	got := decodeSingleObject(t, out)
	if got["command"] != "promote" || got["from"] != "internal" || got["track"] != "beta" {
		t.Fatalf("unexpected object: %v", got)
	}
	if got["status"] != "completed" || got["review"] != "sent" {
		t.Fatalf("status/review = %v/%v", got["status"], got["review"])
	}
	if fake.UpdatedTracks[0] != "beta" {
		t.Fatalf("updated track = %q, want beta", fake.UpdatedTracks[0])
	}
}

func TestPromoteProductionRequiresYes(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: promoteSource}

	_, errOut, code := runWithAPI(t, fake, "promote", "-p", "com.example.app",
		"--from", "beta", "--to", "production")
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "--yes") {
		t.Fatalf("unexpected error: %s", errOut)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called without confirmation: %v", fake.Calls)
	}
}

func TestPromoteTextOutput(t *testing.T) {
	fake := &playtest.Fake{TrackEditFunc: promoteSource}

	out, errOut, code := runWithAPI(t, fake, "promote", "-p", "com.example.app",
		"--from", "internal", "--to", "beta")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}
	if !strings.Contains(out, "promoted com.example.app version 42 from internal to beta") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRolloutManualActionKeepsExitCode(t *testing.T) {
	calls := 0
	fake := &playtest.Fake{
		TrackEditFunc: stagedTrack,
		CommitEditFunc: func(_ context.Context, _ string, _ string, _ play.CommitOptions) error {
			calls++
			if calls == 1 {
				return exitcode.Errorf(exitcode.ManualAction, "changes cannot be sent for review automatically")
			}
			return nil
		},
	}

	out, errOut, code := runWithAPI(t, fake, "rollout", "-p", "com.example.app",
		"-t", "production", "--fraction", "0.2", "--json")
	if code != int(exitcode.ManualAction) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.ManualAction, errOut)
	}

	got := decodeSingleObject(t, out)
	if got["review"] != "notSent" || got["ok"] != true {
		t.Fatalf("unexpected object: %v", got)
	}
	if !strings.Contains(errOut, "Publishing overview") {
		t.Fatalf("missing manual submission instruction: %s", errOut)
	}
}
