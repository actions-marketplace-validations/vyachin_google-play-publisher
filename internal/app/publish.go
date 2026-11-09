// Package app implements gplay use cases on top of the Play API.
package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/release"
)

// Review outcomes reported by Publish.
const (
	ReviewSent      = "sent"
	ReviewNotSent   = "notSent"
	ReviewReplaced  = "replaced"
	ReviewSkipped   = "skipped"
	ReviewValidated = "validated"
)

// PublishOptions describe one publish operation.
type PublishOptions struct {
	Package                 string
	AABPath                 string
	MetadataPath            string
	Track                   string
	Status                  string
	Rollout                 float64
	RolloutSet              bool
	Notes                   map[string]string
	IfExists                release.ExistsPolicy
	ChangesNotSentForReview bool
	InReviewBehavior        string
	DryRun                  bool
}

// PublishResult describes a completed publish operation.
type PublishResult struct {
	Package     string
	Track       string
	Status      string
	VersionCode int64
	SHA256      string
	EditID      string
	Review      string
}

// Publisher runs the publish use case.
type Publisher struct {
	api    play.API
	logger *slog.Logger
}

// NewPublisher creates a Publisher. A nil logger discards logs.
func NewPublisher(api play.API, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Publisher{api: api, logger: logger}
}

// Publish uploads an AAB, declares the release on a track and commits the edit.
func (p *Publisher) Publish(ctx context.Context, opts PublishOptions) (PublishResult, error) {
	if opts.IfExists == "" {
		opts.IfExists = release.ExistsFail
	}

	plan, err := release.NewPlan(release.Input{
		Package:    opts.Package,
		Track:      opts.Track,
		Status:     opts.Status,
		Rollout:    opts.Rollout,
		RolloutSet: opts.RolloutSet,
		Notes:      opts.Notes,
	})
	if err != nil {
		return PublishResult{}, err
	}

	info, err := artifact.Inspect(opts.AABPath)
	if err != nil {
		return PublishResult{}, err
	}

	meta, err := readMetadata(opts)
	if err != nil {
		return PublishResult{}, err
	}

	releases, err := p.api.TrackReleases(ctx, plan.Package, plan.Track)
	if err != nil {
		return PublishResult{}, err
	}
	if meta != nil && versionReleased(releases, meta.VersionCode) {
		return p.alreadyReleased(opts, plan, meta, info)
	}

	editID, err := p.api.CreateEdit(ctx, plan.Package)
	if err != nil {
		return PublishResult{}, err
	}

	committed := false
	defer func() {
		if !committed {
			p.deleteEdit(plan.Package, editID)
		}
	}()

	file, err := os.Open(opts.AABPath)
	if err != nil {
		return PublishResult{}, exitcode.Wrap(exitcode.Usage, err)
	}
	defer func() { _ = file.Close() }()

	bundle, err := p.api.UploadBundle(ctx, plan.Package, editID, file, info.Size)
	if err != nil {
		return PublishResult{}, err
	}
	if meta != nil && bundle.VersionCode != meta.VersionCode {
		return PublishResult{}, exitcode.Errorf(exitcode.Usage,
			"metadata versionCode %d does not match uploaded versionCode %d",
			meta.VersionCode, bundle.VersionCode)
	}

	result := PublishResult{
		Package:     plan.Package,
		Track:       plan.Track,
		Status:      string(plan.Status),
		VersionCode: bundle.VersionCode,
		SHA256:      info.SHA256,
		EditID:      editID,
	}

	if err := p.api.UpdateTrack(ctx, plan.Package, editID, plan.Track, play.Release{
		VersionCodes: []int64{bundle.VersionCode},
		Status:       string(plan.Status),
		UserFraction: plan.UserFraction,
		ReleaseNotes: playNotes(plan.Notes),
	}); err != nil {
		return PublishResult{}, err
	}

	if opts.DryRun {
		if err := p.api.ValidateEdit(ctx, plan.Package, editID); err != nil {
			return PublishResult{}, err
		}
		result.Review = ReviewValidated
		return result, nil
	}

	review := reviewOutcome(opts)
	commitOpts := play.CommitOptions{
		ChangesNotSentForReview: opts.ChangesNotSentForReview,
		InReviewBehavior:        opts.InReviewBehavior,
	}

	commitErr := p.api.CommitEdit(ctx, plan.Package, editID, commitOpts)
	if commitErr == nil {
		committed = true
		result.Review = review
		return result, nil
	}

	if exitcode.From(commitErr) == exitcode.ManualAction && !opts.ChangesNotSentForReview {
		commitOpts.ChangesNotSentForReview = true
		retryErr := p.api.CommitEdit(ctx, plan.Package, editID, commitOpts)
		if retryErr == nil {
			committed = true
			result.Review = ReviewNotSent
			return result, nil
		}
		commitErr = retryErr
	}

	released, verifyErr := p.isReleased(ctx, plan.Package, plan.Track, bundle.VersionCode)
	if verifyErr == nil && released {
		committed = true
		result.Review = review
		return result, nil
	}
	if verifyErr != nil {
		return PublishResult{}, exitcode.WithHint(commitErr,
			"commit state is unknown: check the track before retrying")
	}

	if exitcode.From(commitErr) == exitcode.Transient {
		retryErr := p.api.CommitEdit(ctx, plan.Package, editID, commitOpts)
		if retryErr == nil {
			committed = true
			result.Review = review
			return result, nil
		}
		commitErr = retryErr
	}

	return PublishResult{}, commitErr
}

func (p *Publisher) alreadyReleased(
	opts PublishOptions,
	plan release.Plan,
	meta *artifact.Metadata,
	info artifact.Info,
) (PublishResult, error) {
	if opts.IfExists == release.ExistsSkip {
		return PublishResult{
			Package:     plan.Package,
			Track:       plan.Track,
			Status:      string(plan.Status),
			VersionCode: meta.VersionCode,
			SHA256:      info.SHA256,
			Review:      ReviewSkipped,
		}, nil
	}

	return PublishResult{}, exitcode.WithHint(
		exitcode.Errorf(exitcode.Conflict,
			"versionCode %d is already on track %s", meta.VersionCode, plan.Track),
		"bump versionCode and rebuild the AAB, or pass --if-exists=skip",
	)
}

func (p *Publisher) isReleased(ctx context.Context, pkg, track string, versionCode int64) (bool, error) {
	releases, err := p.api.TrackReleases(ctx, pkg, track)
	if err != nil {
		return false, err
	}
	return versionReleased(releases, versionCode), nil
}

func (p *Publisher) deleteEdit(pkg, editID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := p.api.DeleteEdit(ctx, pkg, editID); err != nil {
		p.logger.Warn("failed to delete edit", "editId", editID, "error", err)
		return
	}
	p.logger.Debug("edit deleted", "editId", editID)
}

func readMetadata(opts PublishOptions) (*artifact.Metadata, error) {
	if opts.MetadataPath == "" {
		return nil, nil
	}

	meta, err := artifact.ReadMetadata(opts.MetadataPath)
	if err != nil {
		return nil, err
	}
	if err := meta.VerifyPackage(opts.Package); err != nil {
		return nil, err
	}
	return &meta, nil
}

func versionReleased(releases []play.ReleaseSummary, versionCode int64) bool {
	for _, rel := range releases {
		for _, code := range rel.VersionCodes {
			if code == versionCode {
				return true
			}
		}
	}
	return false
}

func playNotes(notes []release.Note) []play.LocalizedText {
	result := make([]play.LocalizedText, 0, len(notes))
	for _, note := range notes {
		result = append(result, play.LocalizedText{Language: note.Language, Text: note.Text})
	}
	return result
}

func reviewOutcome(opts PublishOptions) string {
	if opts.ChangesNotSentForReview {
		return ReviewNotSent
	}
	if opts.InReviewBehavior == play.InReviewReplace {
		return ReviewReplaced
	}
	return ReviewSent
}
