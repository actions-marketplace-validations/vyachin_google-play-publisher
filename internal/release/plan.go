// Package release defines and validates a desired Google Play release.
package release

import (
	"math"
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
	StatusHalted     Status = "halted"
	StatusCompleted  Status = "completed"
)

// MaxNoteLength is the maximum length of release notes per locale.
const MaxNoteLength = 500

// MaxInAppUpdatePriority is the highest in-app update priority Play accepts.
const MaxInAppUpdatePriority = 5

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
	// InAppUpdatePriority is the Play in-app update priority; 0 means unset.
	InAppUpdatePriority int
	// Countries and IncludeRestOfWorld describe optional country targeting.
	Countries          []string
	IncludeRestOfWorld bool
}

// Input holds raw values collected from CLI flags.
type Input struct {
	Package string
	Track   string
	Status  string
	// Rollout is nil when no staged rollout was requested.
	Rollout *float64
	Notes   map[string]string
	// InAppUpdatePriority is the Play in-app update priority; 0 means unset.
	InAppUpdatePriority int
	// Countries and IncludeRestOfWorld request country targeting. Play accepts
	// them only for an inProgress release on the production track.
	Countries          []string
	IncludeRestOfWorld bool
}

var (
	packagePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)
	trackPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:_-]*$`)
	localePattern  = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

// NewPlan validates CLI input and builds a release plan.
func NewPlan(in Input) (Plan, error) {
	track, err := ValidateTarget(in.Package, in.Track)
	if err != nil {
		return Plan{}, err
	}

	status, err := resolveStatus(in.Status, in.Rollout)
	if err != nil {
		return Plan{}, err
	}

	notes, err := validateNotes(in.Notes)
	if err != nil {
		return Plan{}, err
	}

	priority, err := validatePriority(in.InAppUpdatePriority)
	if err != nil {
		return Plan{}, err
	}

	countries, restOfWorld, err := validateTargeting(track, status, in.Countries, in.IncludeRestOfWorld)
	if err != nil {
		return Plan{}, err
	}

	fraction := 0.0
	if in.Rollout != nil {
		fraction = *in.Rollout
	}

	return Plan{
		Package:             in.Package,
		Track:               track,
		Status:              status,
		UserFraction:        fraction,
		Notes:               notes,
		InAppUpdatePriority: priority,
		Countries:           countries,
		IncludeRestOfWorld:  restOfWorld,
	}, nil
}

func resolveStatus(raw string, rollout *float64) (Status, error) {
	rolloutSet := rollout != nil
	if rolloutSet && (math.IsNaN(*rollout) || math.IsInf(*rollout, 0) || *rollout <= 0 || *rollout >= 1) {
		return "", exitcode.Errorf(exitcode.Usage,
			"--rollout must be between 0 and 1 (exclusive), got %v", *rollout)
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
		if err := ValidateLocale(locale); err != nil {
			return nil, err
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

// ValidateLocale checks a BCP-47 language tag such as "en-US" or "de".
func ValidateLocale(locale string) error {
	if !localePattern.MatchString(locale) {
		return exitcode.Errorf(exitcode.Usage, "invalid locale %q: want a BCP-47 tag such as en-US", locale)
	}
	return nil
}

// ValidateTrackName checks a Play track name, including form-factor prefixes
// such as "wear:production".
func ValidateTrackName(track string) error {
	if !trackPattern.MatchString(track) {
		return exitcode.Errorf(exitcode.Usage, "invalid track name %q", track)
	}
	return nil
}

// ValidateTarget checks the package and track names and returns the effective
// track name (empty defaults to "internal"). Commands that do not publish an
// AAB, such as rollout and promote, validate their target with it before any
// API call.
func ValidateTarget(pkg, track string) (string, error) {
	if !packagePattern.MatchString(pkg) {
		return "", exitcode.Errorf(exitcode.Usage, "invalid package name %q", pkg)
	}

	effective := strings.TrimSpace(track)
	if effective == "" {
		effective = "internal"
	}
	if err := ValidateTrackName(effective); err != nil {
		return "", err
	}
	return effective, nil
}

// validatePriority checks the in-app update priority range.
func validatePriority(priority int) (int, error) {
	if priority < 0 || priority > MaxInAppUpdatePriority {
		return 0, exitcode.Errorf(exitcode.Usage,
			"--in-app-update-priority must be between 0 and %d, got %d",
			MaxInAppUpdatePriority, priority)
	}
	return priority, nil
}

// validateTargeting normalizes country codes and enforces the Play rule that
// country targeting is accepted only for an inProgress release on production.
func validateTargeting(track string, status Status, raw []string, restOfWorld bool) ([]string, bool, error) {
	countries := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		code := strings.ToUpper(strings.TrimSpace(item))
		if !countryPattern.MatchString(code) {
			return nil, false, exitcode.Errorf(exitcode.Usage,
				"invalid country code %q: want a two-letter CLDR code, for example DE", item)
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		countries = append(countries, code)
	}
	sort.Strings(countries)

	if len(countries) == 0 && !restOfWorld {
		return nil, false, nil
	}
	if len(countries) == 0 {
		return nil, false, exitcode.Errorf(exitcode.Usage,
			"--include-rest-of-world requires at least one --country")
	}
	if status != StatusInProgress || track != "production" {
		return nil, false, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage,
				"country targeting is only supported for an inProgress release on the production track"),
			"combine --country with --track=production --status=inProgress --rollout <fraction>, or drop --country")
	}
	return countries, restOfWorld, nil
}

// RolloutPlan is a validated change to an existing track release.
type RolloutPlan struct {
	Status       Status
	UserFraction float64
}

// NewRolloutPlan validates a rollout change. Exactly one of a fraction, a halt
// and a completion must be requested.
func NewRolloutPlan(fraction *float64, halt, complete bool) (RolloutPlan, error) {
	selected := 0
	if fraction != nil {
		selected++
	}
	if halt {
		selected++
	}
	if complete {
		selected++
	}

	switch {
	case selected == 0:
		return RolloutPlan{}, exitcode.Errorf(exitcode.Usage,
			"one of --fraction, --halt or --complete is required")
	case selected > 1:
		return RolloutPlan{}, exitcode.Errorf(exitcode.Usage,
			"--fraction, --halt and --complete are mutually exclusive")
	}

	switch {
	case complete:
		return RolloutPlan{Status: StatusCompleted}, nil
	case halt:
		return RolloutPlan{Status: StatusHalted}, nil
	}

	value := *fraction
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value >= 1 {
		return RolloutPlan{}, exitcode.Errorf(exitcode.Usage,
			"--fraction must be between 0 and 1 (exclusive), got %v", value)
	}
	return RolloutPlan{Status: StatusInProgress, UserFraction: value}, nil
}
