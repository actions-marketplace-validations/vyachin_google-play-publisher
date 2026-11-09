// Package release defines and validates a desired Google Play release.
package release

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// Status is a Google Play release status.
type Status string

// Supported release statuses.
const (
	StatusDraft      Status = "draft"
	StatusInProgress Status = "inProgress"
	StatusCompleted  Status = "completed"
)

// MaxNoteLength is the maximum length of release notes per locale.
const MaxNoteLength = 500

// Note is a release note for one locale.
type Note struct {
	Language string
	Text     string
}

// Plan is a validated release ready to be applied to a track.
type Plan struct {
	Package      string
	Track        string
	Status       Status
	UserFraction float64
	Notes        []Note
}

// Input holds raw values collected from CLI flags.
type Input struct {
	Package    string
	Track      string
	Status     string
	Rollout    float64
	RolloutSet bool
	Notes      map[string]string
}

var (
	packagePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)
	trackPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:_-]*$`)
	localePattern  = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)
)

// NewPlan validates CLI input and builds a release plan.
func NewPlan(in Input) (Plan, error) {
	if !packagePattern.MatchString(in.Package) {
		return Plan{}, exitcode.Errorf(exitcode.Usage, "invalid package name %q", in.Package)
	}

	track := strings.TrimSpace(in.Track)
	if track == "" {
		track = "internal"
	}
	if !trackPattern.MatchString(track) {
		return Plan{}, exitcode.Errorf(exitcode.Usage, "invalid track name %q", track)
	}

	status, err := resolveStatus(in.Status, in.Rollout, in.RolloutSet)
	if err != nil {
		return Plan{}, err
	}

	notes, err := validateNotes(in.Notes)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Package:      in.Package,
		Track:        track,
		Status:       status,
		UserFraction: in.Rollout,
		Notes:        notes,
	}, nil
}

func resolveStatus(raw string, rollout float64, rolloutSet bool) (Status, error) {
	if rolloutSet && (rollout <= 0 || rollout >= 1) {
		return "", exitcode.Errorf(exitcode.Usage,
			"--rollout must be between 0 and 1 (exclusive), got %v", rollout)
	}

	status := Status(strings.TrimSpace(raw))
	if status == "" {
		if rolloutSet {
			return StatusInProgress, nil
		}
		return StatusCompleted, nil
	}

	switch status {
	case StatusDraft, StatusCompleted:
		if rolloutSet {
			return "", exitcode.Errorf(exitcode.Usage,
				"--rollout cannot be combined with --status=%s", status)
		}
	case StatusInProgress:
		if !rolloutSet {
			return "", exitcode.Errorf(exitcode.Usage,
				"--status=inProgress requires --rollout between 0 and 1")
		}
	default:
		return "", exitcode.Errorf(exitcode.Usage,
			"invalid --status %q: want draft, inProgress or completed", raw)
	}

	return status, nil
}

func validateNotes(raw map[string]string) ([]Note, error) {
	notes := make([]Note, 0, len(raw))
	for locale, text := range raw {
		if !localePattern.MatchString(locale) {
			return nil, exitcode.Errorf(exitcode.Usage, "invalid release notes locale %q", locale)
		}

		text = strings.TrimSpace(text)
		if text == "" {
			return nil, exitcode.Errorf(exitcode.Usage, "release notes for %q are empty", locale)
		}
		if length := utf8.RuneCountInString(text); length > MaxNoteLength {
			return nil, exitcode.Errorf(exitcode.Usage,
				"release notes for %q are %d characters, limit is %d", locale, length, MaxNoteLength)
		}

		notes = append(notes, Note{Language: locale, Text: text})
	}

	sort.Slice(notes, func(i, j int) bool { return notes[i].Language < notes[j].Language })
	return notes, nil
}

// ExistsPolicy controls what to do when the version code is already released.
type ExistsPolicy string

// Supported policies for an already released version code.
const (
	ExistsFail ExistsPolicy = "fail"
	ExistsSkip ExistsPolicy = "skip"
)

// ParseExistsPolicy validates the --if-exists flag.
func ParseExistsPolicy(raw string) (ExistsPolicy, error) {
	switch policy := ExistsPolicy(strings.TrimSpace(raw)); policy {
	case "", ExistsFail:
		return ExistsFail, nil
	case ExistsSkip:
		return ExistsSkip, nil
	default:
		return "", exitcode.Errorf(exitcode.Usage,
			"invalid --if-exists %q: want fail or skip", raw)
	}
}
