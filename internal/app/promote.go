package app

import (
	"context"
	"strconv"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/release"
	"github.com/vyachin/google-play-publisher/internal/storemeta"
)

// PromoteOptions describe a promotion: the version codes of a release on one
// track are declared as a new release on another track, without uploading a
// bundle again.
type PromoteOptions struct {
	Package             string
	From                string
	To                  string
	VersionCode         int64
	Status              string
	Rollout             *float64
	Notes               map[string]string
	MetadataDir         string
	ReleaseName         string
	InAppUpdatePriority int
	Countries           []string
	IncludeRestOfWorld  bool
	// Confirm acknowledges a promotion to the production track.
	Confirm                 bool
	ChangesNotSentForReview bool
	InReviewBehavior        string
	DryRun                  bool
}

// PromoteResult describes an applied promotion.
type PromoteResult struct {
	Package      string
	From         string
	To           string
	Status       string
	VersionCodes []int64
	UserFraction float64
	EditID       string
	Review       string
	// Metadata is nil when --metadata-dir was not used.
	Metadata *MetadataResult
}

// PreparedPromote holds a promotion validated by PreparePromote.
type PreparedPromote struct {
	opts  PromoteOptions
	plan  release.Plan
	store *storemeta.Metadata
}

// PreparePromote validates every local input. It fails with exit code 2 before
// an API client is created, and before an edit is opened, so a bad promotion
// cannot reach Play.
func PreparePromote(opts PromoteOptions) (PreparedPromote, error) {
	pkg := opts.Package
	if pkg == "" {
		return PreparedPromote{}, exitcode.Errorf(exitcode.Usage, "--package is required")
	}

	if opts.From == "" {
		return PreparedPromote{}, exitcode.Errorf(exitcode.Usage, "--from is required")
	}
	from, err := release.ValidateTarget(pkg, opts.From)
	if err != nil {
		return PreparedPromote{}, err
	}
	if opts.To == "" {
		return PreparedPromote{}, exitcode.Errorf(exitcode.Usage, "--to is required")
	}
	to, err := release.ValidateTarget(pkg, opts.To)
	if err != nil {
		return PreparedPromote{}, err
	}
	if from == to {
		return PreparedPromote{}, exitcode.Errorf(exitcode.Usage,
			"--from and --to must name different tracks, both are %q", from)
	}
	if to == "production" && !opts.Confirm {
		return PreparedPromote{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage,
				"promoting to the production track requires --yes"),
			"re-run with --yes after checking the version and the rollout fraction")
	}

	opts.Package, opts.From, opts.To = pkg, from, to

	if opts.VersionCode < 0 {
		return PreparedPromote{}, exitcode.Errorf(exitcode.Usage,
			"--version-code must be positive, got %d", opts.VersionCode)
	}
	if err := validateCommitSettings(opts.ChangesNotSentForReview, opts.DryRun, opts.InReviewBehavior); err != nil {
		return PreparedPromote{}, err
	}

	if opts.MetadataDir != "" && len(opts.Notes) > 0 {
		return PreparedPromote{}, exitcode.Errorf(exitcode.Usage,
			"release notes cannot be supplied together with --metadata-dir")
	}

	notes := opts.Notes
	if opts.MetadataDir != "" && opts.VersionCode > 0 {
		notes, err = readChangelogDir(opts.MetadataDir, opts.VersionCode)
		if err != nil {
			return PreparedPromote{}, err
		}
	}

	store, err := storemeta.Read(opts.MetadataDir)
	if err != nil {
		return PreparedPromote{}, err
	}
	if opts.MetadataDir != "" && store == nil && len(notes) == 0 {
		if opts.VersionCode == 0 {
			return PreparedPromote{}, exitcode.WithHint(
				exitcode.Errorf(exitcode.Usage,
					"no store metadata found under %s", opts.MetadataDir),
				"pass --version-code to read a changelog-only tree, or add listings, images or testers")
		}
		return PreparedPromote{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage,
				"no store metadata or changelog for versionCode %d under %s", opts.VersionCode, opts.MetadataDir),
			"add <dir>/<locale>/changelogs/"+strconv.FormatInt(opts.VersionCode, 10)+".txt, or a full metadata tree")
	}

	plan, err := release.NewPlan(release.Input{
		Package:             pkg,
		Track:               to,
		Status:              opts.Status,
		Rollout:             opts.Rollout,
		Notes:               notes,
		InAppUpdatePriority: opts.InAppUpdatePriority,
		Countries:           opts.Countries,
		IncludeRestOfWorld:  opts.IncludeRestOfWorld,
	})
	if err != nil {
		return PreparedPromote{}, err
	}

	return PreparedPromote{opts: opts, plan: plan, store: store}, nil
}

// Track returns the validated destination track.
func (p PreparedPromote) Track() string { return p.opts.To }

// Source returns the validated source track.
func (p PreparedPromote) Source() string { return p.opts.From }

// Promote prepares and applies a promotion.
func (p *Publisher) Promote(ctx context.Context, opts PromoteOptions) (PromoteResult, error) {
	prepared, err := PreparePromote(opts)
	if err != nil {
		return PromoteResult{}, err
	}
	return p.PromotePrepared(ctx, prepared)
}

// PromotePrepared applies a promotion validated by PreparePromote.
func (p *Publisher) PromotePrepared(ctx context.Context, prepared PreparedPromote) (PromoteResult, error) {
	opts, plan := prepared.opts, prepared.plan

	p.logger.Info("promoting",
		"package", opts.Package,
		"from", opts.From,
		"to", opts.To,
		"status", plan.Status,
		"versionCode", opts.VersionCode,
	)

	editID, err := p.api.CreateEdit(ctx, opts.Package)
	if err != nil {
		return PromoteResult{}, err
	}
	p.logger.Debug("edit created", "editId", editID)

	skipCleanup := false
	defer func() {
		if !skipCleanup {
			_ = p.cleanupEdit(ctx, opts.Package, editID)
		}
	}()

	source, err := p.api.TrackEdit(ctx, opts.Package, editID, opts.From)
	if err != nil {
		return PromoteResult{}, err
	}
	origin, err := selectPromoteRelease(source.Releases, opts.VersionCode)
	if err != nil {
		return PromoteResult{}, err
	}

	destination, err := p.api.TrackEdit(ctx, opts.Package, editID, opts.To)
	if err != nil {
		if exitcode.From(err) == exitcode.NotFound {
			return PromoteResult{}, exitcode.WithHint(err,
				"check the --to track name; a custom closed track must exist in Play Console")
		}
		return PromoteResult{}, err
	}
	already := releasedCodes(destination.Releases)
	for _, code := range origin.VersionCodes {
		if already[code] {
			return PromoteResult{}, exitcode.WithHint(
				exitcode.Errorf(exitcode.Conflict,
					"version %s is already on track %s", codesString(origin.VersionCodes), opts.To),
				"use 'rollout' to change the fraction or 'status' to inspect the track")
		}
	}

	target := play.Release{
		VersionCodes:        append([]int64(nil), origin.VersionCodes...),
		Status:              string(plan.Status),
		UserFraction:        plan.UserFraction,
		ReleaseNotes:        promoteNotes(plan.Notes, origin.ReleaseNotes),
		Name:                opts.ReleaseName,
		InAppUpdatePriority: int64(plan.InAppUpdatePriority),
		CountryTargeting:    releaseTargeting(plan),
	}
	if target.Name == "" {
		target.Name = origin.Name
	}

	applied, err := p.applyMetadata(ctx, opts.Package, editID, prepared.store)
	if err != nil {
		return PromoteResult{}, err
	}

	if err := p.api.UpdateTrack(ctx, opts.Package, editID, opts.To, target); err != nil {
		return PromoteResult{}, err
	}

	result := PromoteResult{
		Package:      opts.Package,
		From:         opts.From,
		To:           opts.To,
		Status:       string(plan.Status),
		VersionCodes: target.VersionCodes,
		UserFraction: target.UserFraction,
		EditID:       editID,
		Metadata:     applied,
	}

	if opts.DryRun {
		skipCleanup = true // finishDryRun reports its own failure; do not try twice.
		if err := p.finishDryRun(ctx, opts.Package, editID); err != nil {
			return PromoteResult{}, err
		}
		result.Review = ReviewValidated
		return result, nil
	}

	outcome := p.commit(ctx, commitSettings{
		ChangesNotSentForReview: opts.ChangesNotSentForReview,
		InReviewBehavior:        opts.InReviewBehavior,
	}, commitTarget{
		Package:      opts.Package,
		Track:        opts.To,
		Status:       plan.Status,
		VersionCodes: result.VersionCodes,
	}, editID)

	switch outcome.kind {
	case commitCommitted:
		skipCleanup = true
		result.Review = outcome.review
		if outcome.manual {
			p.logger.Info("promotion saved for manual review",
				"versionCodes", result.VersionCodes, "editId", editID)
			return result, exitcode.WithHint(
				exitcode.Errorf(exitcode.ManualAction, "changes saved but not submitted for review"),
				"open Play Console -> Publishing overview and send the changes for review")
		}
		p.logger.Info("promoted",
			"from", result.From, "to", result.To,
			"versionCodes", result.VersionCodes, "review", outcome.review)
		return result, nil
	case commitUnknown:
		skipCleanup = true
		p.logger.Warn("commit state is unknown", "editId", editID, "error", outcome.err)
		return PromoteResult{}, outcome.err
	default:
		return PromoteResult{}, outcome.err
	}
}

// promoteNotes returns explicit notes, or the source release notes when the
// caller supplied none: a promotion should not silently drop the changelog.
func promoteNotes(notes []release.Note, source []play.LocalizedText) []play.LocalizedText {
	if len(notes) == 0 {
		return source
	}
	return playNotes(notes)
}

// selectPromoteRelease picks the release to promote. An explicit version code
// must be present on the source track; otherwise the release with the highest
// version code wins, ignoring drafts.
func selectPromoteRelease(releases []play.Release, versionCode int64) (play.Release, error) {
	if versionCode > 0 {
		for _, rel := range releases {
			if !containsCode(rel.VersionCodes, versionCode) {
				continue
			}
			if rel.Status == play.StatusDraft {
				return play.Release{}, exitcode.WithHint(
					exitcode.Errorf(exitcode.Usage,
						"version %d is a draft on the source track", versionCode),
					"publish the draft release before promoting it")
			}
			return rel, nil
		}
		return play.Release{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.NotFound,
				"version %d is not on the source track", versionCode),
			"check the source track with 'status'")
	}

	var (
		best     play.Release
		bestCode int64 = -1
	)
	for _, rel := range releases {
		if len(rel.VersionCodes) == 0 || rel.Status == play.StatusDraft {
			continue
		}
		if code := maxCode(rel.VersionCodes); code > bestCode {
			best, bestCode = rel, code
		}
	}
	if bestCode < 0 {
		return play.Release{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.NotFound, "no release to promote on the source track"),
			"publish a release to the source track first")
	}
	return best, nil
}

// releasedCodes collects the version codes that already appear on a track.
func releasedCodes(releases []play.Release) map[int64]bool {
	codes := make(map[int64]bool, len(releases))
	for _, rel := range releases {
		for _, code := range rel.VersionCodes {
			codes[code] = true
		}
	}
	return codes
}

func maxCode(codes []int64) int64 {
	highest := int64(-1)
	for _, code := range codes {
		if code > highest {
			highest = code
		}
	}
	return highest
}
