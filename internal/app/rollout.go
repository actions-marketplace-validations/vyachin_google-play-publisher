package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/release"
)

// RolloutOptions describe a change to an existing track release. No bundle is
// uploaded: the command reads the release inside the edit, changes only its
// status and fraction, and commits the edit.
type RolloutOptions struct {
	Package     string
	Track       string
	VersionCode int64
	// Fraction is nil when --fraction was not supplied.
	Fraction                *float64
	Halt                    bool
	Complete                bool
	ChangesNotSentForReview bool
	InReviewBehavior        string
	DryRun                  bool
}

// RolloutResult describes an applied rollout change.
type RolloutResult struct {
	Package      string
	Track        string
	Status       string
	VersionCodes []int64
	UserFraction float64
	EditID       string
	Review       string
}

// PreparedRollout holds a rollout change validated by PrepareRollout.
type PreparedRollout struct {
	opts RolloutOptions
	plan release.RolloutPlan
}

// PrepareRollout validates every local input, so an invalid request fails with
// exit code 2 before an API client is constructed or an edit is created.
func PrepareRollout(opts RolloutOptions) (PreparedRollout, error) {
	track, err := release.ValidateTarget(opts.Package, opts.Track)
	if err != nil {
		return PreparedRollout{}, err
	}
	opts.Track = track

	if opts.VersionCode < 0 {
		return PreparedRollout{}, exitcode.Errorf(exitcode.Usage,
			"--version-code must be positive, got %d", opts.VersionCode)
	}
	if err := validateCommitSettings(opts.ChangesNotSentForReview, opts.DryRun, opts.InReviewBehavior); err != nil {
		return PreparedRollout{}, err
	}

	plan, err := release.NewRolloutPlan(opts.Fraction, opts.Halt, opts.Complete)
	if err != nil {
		return PreparedRollout{}, err
	}

	return PreparedRollout{opts: opts, plan: plan}, nil
}

// Track returns the validated track name for reporting.
func (p PreparedRollout) Track() string { return p.opts.Track }

// Rollout prepares and applies a rollout change.
func (p *Publisher) Rollout(ctx context.Context, opts RolloutOptions) (RolloutResult, error) {
	prepared, err := PrepareRollout(opts)
	if err != nil {
		return RolloutResult{}, err
	}
	return p.RolloutPrepared(ctx, prepared)
}

// RolloutPrepared applies a rollout change validated by PrepareRollout.
func (p *Publisher) RolloutPrepared(ctx context.Context, prepared PreparedRollout) (RolloutResult, error) {
	opts, plan := prepared.opts, prepared.plan

	p.logger.Info("updating rollout",
		"package", opts.Package,
		"track", opts.Track,
		"status", plan.Status,
		"versionCode", opts.VersionCode,
	)

	editID, err := p.api.CreateEdit(ctx, opts.Package)
	if err != nil {
		return RolloutResult{}, err
	}
	p.logger.Debug("edit created", "editId", editID)

	skipCleanup := false
	defer func() {
		if !skipCleanup {
			_ = p.cleanupEdit(ctx, opts.Package, editID)
		}
	}()

	state, err := p.api.TrackEdit(ctx, opts.Package, editID, opts.Track)
	if err != nil {
		return RolloutResult{}, err
	}

	current, err := selectRolloutRelease(state.Releases, opts.VersionCode)
	if err != nil {
		return RolloutResult{}, err
	}
	p.logger.Debug("rollout target",
		"versionCodes", current.VersionCodes, "status", current.Status, "fraction", current.UserFraction)

	updated := applyRollout(current, plan)
	if err := p.api.UpdateTrack(ctx, opts.Package, editID, opts.Track, updated); err != nil {
		return RolloutResult{}, err
	}

	result := RolloutResult{
		Package:      opts.Package,
		Track:        opts.Track,
		Status:       string(plan.Status),
		VersionCodes: append([]int64(nil), updated.VersionCodes...),
		UserFraction: updated.UserFraction,
		EditID:       editID,
	}

	if opts.DryRun {
		skipCleanup = true // finishDryRun reports its own failure; do not try twice.
		if err := p.finishDryRun(ctx, opts.Package, editID); err != nil {
			return RolloutResult{}, err
		}
		result.Review = ReviewValidated
		return result, nil
	}

	outcome := p.commit(ctx, commitSettings{
		ChangesNotSentForReview: opts.ChangesNotSentForReview,
		InReviewBehavior:        opts.InReviewBehavior,
	}, commitTarget{
		Package:      opts.Package,
		Track:        opts.Track,
		Status:       plan.Status,
		VersionCodes: result.VersionCodes,
	}, editID)

	switch outcome.kind {
	case commitCommitted:
		skipCleanup = true
		result.Review = outcome.review
		if outcome.manual {
			p.logger.Info("rollout saved for manual review",
				"versionCodes", result.VersionCodes, "editId", editID)
			return result, exitcode.WithHint(
				exitcode.Errorf(exitcode.ManualAction, "changes saved but not submitted for review"),
				"open Play Console -> Publishing overview and send the changes for review")
		}
		p.logger.Info("rollout updated",
			"status", result.Status, "versionCodes", result.VersionCodes, "review", outcome.review)
		return result, nil
	case commitUnknown:
		skipCleanup = true
		p.logger.Warn("commit state is unknown", "editId", editID, "error", outcome.err)
		return RolloutResult{}, outcome.err
	default:
		return RolloutResult{}, outcome.err
	}
}

// selectRolloutRelease picks the release a rollout change applies to. An
// explicit version code wins; otherwise exactly one release must match the
// statuses the change can act on.
func selectRolloutRelease(releases []play.Release, versionCode int64) (play.Release, error) {
	if versionCode > 0 {
		for _, rel := range releases {
			if containsCode(rel.VersionCodes, versionCode) {
				return rel, nil
			}
		}
		return play.Release{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.NotFound, "version %d is not on the track", versionCode),
			"run 'status' to list the releases on the track")
	}

	wanted := rolloutCandidateStatuses()
	matches := make([]play.Release, 0, len(releases))
	for _, rel := range releases {
		if len(rel.VersionCodes) == 0 || !containsString(wanted, rel.Status) {
			continue
		}
		matches = append(matches, rel)
	}

	switch len(matches) {
	case 0:
		return play.Release{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.NotFound, "no %s release on the track", describeStatuses(wanted)),
			"publish a staged release first, or pass --version-code")
	case 1:
		return matches[0], nil
	}

	return play.Release{}, exitcode.Errorf(exitcode.Usage,
		"several releases match (%s); pass --version-code", describeReleases(matches))
}

// rolloutCandidateStatuses lists the release statuses a change may act on.
// Increasing the fraction after a halt resumes the release and a halted release
// can still be completed, so both partial rollouts are candidates. A draft or a
// fully completed release is never changed implicitly.
func rolloutCandidateStatuses() []string {
	return []string{play.StatusInProgress, play.StatusHalted}
}

// applyRollout returns a copy of the current release with the requested status
// and fraction. Every other field is preserved, because tracks.update replaces
// the release resource.
func applyRollout(current play.Release, plan release.RolloutPlan) play.Release {
	updated := current
	updated.Status = string(plan.Status)
	switch plan.Status {
	case release.StatusCompleted:
		updated.UserFraction = 0
	case release.StatusHalted:
		// Play keeps the fraction of a halted staged release, so a resume can
		// continue from it. A full release has no fraction to keep.
	default:
		updated.UserFraction = plan.UserFraction
	}
	return updated
}

func containsCode(codes []int64, wanted int64) bool {
	for _, code := range codes {
		if code == wanted {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func describeStatuses(statuses []string) string {
	return strings.Join(statuses, " or ")
}

func describeReleases(releases []play.Release) string {
	parts := make([]string, 0, len(releases))
	for _, rel := range releases {
		parts = append(parts, "version "+codesString(rel.VersionCodes)+" ("+rel.Status+")")
	}
	return strings.Join(parts, ", ")
}

func codesString(codes []int64) string {
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, strconv.FormatInt(code, 10))
	}
	return strings.Join(parts, ",")
}
