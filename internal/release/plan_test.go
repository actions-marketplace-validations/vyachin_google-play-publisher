package release_test

import (
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

func TestNewPlanValid(t *testing.T) {
	tests := []struct {
		name         string
		in           release.Input
		wantStatus   release.Status
		wantFraction float64
		wantTrack    string
	}{
		{
			name:       "defaults to completed on internal",
			in:         release.Input{Package: "com.example.app"},
			wantStatus: release.StatusCompleted,
			wantTrack:  "internal",
		},
		{
			name:       "explicit draft",
			in:         release.Input{Package: "com.example.app", Status: "draft"},
			wantStatus: release.StatusDraft,
			wantTrack:  "internal",
		},
		{
			name:         "rollout implies inProgress",
			in:           release.Input{Package: "com.example.app", Rollout: 0.05, RolloutSet: true},
			wantStatus:   release.StatusInProgress,
			wantFraction: 0.05,
			wantTrack:    "internal",
		},
		{
			name: "explicit inProgress",
			in: release.Input{
				Package:    "com.example.app",
				Status:     "inProgress",
				Rollout:    0.2,
				RolloutSet: true,
			},
			wantStatus:   release.StatusInProgress,
			wantFraction: 0.2,
			wantTrack:    "internal",
		},
		{
			name:       "custom track",
			in:         release.Input{Package: "com.example.app", Track: "wear:production"},
			wantStatus: release.StatusCompleted,
			wantTrack:  "wear:production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := release.NewPlan(tt.in)
			if err != nil {
				t.Fatalf("NewPlan: %v", err)
			}
			if plan.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", plan.Status, tt.wantStatus)
			}
			if plan.UserFraction != tt.wantFraction {
				t.Errorf("UserFraction = %v, want %v", plan.UserFraction, tt.wantFraction)
			}
			if plan.Track != tt.wantTrack {
				t.Errorf("Track = %q, want %q", plan.Track, tt.wantTrack)
			}
		})
	}
}

func TestNewPlanErrors(t *testing.T) {
	tests := []struct {
		name string
		in   release.Input
		want string
	}{
		{
			name: "invalid package",
			in:   release.Input{Package: "com"},
			want: "package",
		},
		{
			name: "empty package",
			in:   release.Input{},
			want: "package",
		},
		{
			name: "invalid track",
			in:   release.Input{Package: "com.example.app", Track: "internal!"},
			want: "track",
		},
		{
			name: "unknown status",
			in:   release.Input{Package: "com.example.app", Status: "beta"},
			want: "status",
		},
		{
			name: "completed with rollout",
			in:   release.Input{Package: "com.example.app", Status: "completed", Rollout: 0.1, RolloutSet: true},
			want: "rollout",
		},
		{
			name: "inProgress without rollout",
			in:   release.Input{Package: "com.example.app", Status: "inProgress"},
			want: "rollout",
		},
		{
			name: "rollout is zero",
			in:   release.Input{Package: "com.example.app", Rollout: 0, RolloutSet: true},
			want: "rollout",
		},
		{
			name: "rollout is one",
			in:   release.Input{Package: "com.example.app", Rollout: 1, RolloutSet: true},
			want: "rollout",
		},
		{
			name: "rollout is negative",
			in:   release.Input{Package: "com.example.app", Rollout: -0.1, RolloutSet: true},
			want: "rollout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := release.NewPlan(tt.in)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := exitcode.From(err); got != exitcode.Usage {
				t.Fatalf("code = %d, want %d", got, exitcode.Usage)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestNewPlanNotes(t *testing.T) {
	plan, err := release.NewPlan(release.Input{
		Package: "com.example.app",
		Notes: map[string]string{
			"de-DE": "Fehlerbehebungen",
			"en-US": "Bug fixes",
		},
	})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}

	if len(plan.Notes) != 2 {
		t.Fatalf("notes = %d, want 2", len(plan.Notes))
	}
	if plan.Notes[0].Language != "de-DE" || plan.Notes[1].Language != "en-US" {
		t.Fatalf("notes are not sorted: %#v", plan.Notes)
	}
}

func TestNewPlanNoteLimits(t *testing.T) {
	tests := []struct {
		name  string
		notes map[string]string
		want  string
	}{
		{
			name:  "invalid locale",
			notes: map[string]string{"en_US": "text"},
			want:  "locale",
		},
		{
			name:  "empty text",
			notes: map[string]string{"en-US": "   "},
			want:  "empty",
		},
		{
			name:  "too long",
			notes: map[string]string{"en-US": strings.Repeat("a", release.MaxNoteLength+1)},
			want:  "limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := release.NewPlan(release.Input{Package: "com.example.app", Notes: tt.notes})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := exitcode.From(err); got != exitcode.Usage {
				t.Fatalf("code = %d, want %d", got, exitcode.Usage)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestMaxNoteLengthBoundary(t *testing.T) {
	_, err := release.NewPlan(release.Input{
		Package: "com.example.app",
		Notes:   map[string]string{"ru-RU": strings.Repeat("é", release.MaxNoteLength)},
	})
	if err != nil {
		t.Fatalf("500 runes must be accepted: %v", err)
	}
}

func TestParseExistsPolicy(t *testing.T) {
	tests := []struct {
		raw     string
		want    release.ExistsPolicy
		wantErr bool
	}{
		{raw: "", want: release.ExistsFail},
		{raw: "fail", want: release.ExistsFail},
		{raw: "skip", want: release.ExistsSkip},
		{raw: "overwrite", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := release.ParseExistsPolicy(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if code := exitcode.From(err); code != exitcode.Usage {
					t.Fatalf("code = %d, want %d", code, exitcode.Usage)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExistsPolicy: %v", err)
			}
			if got != tt.want {
				t.Fatalf("policy = %q, want %q", got, tt.want)
			}
		})
	}
}
