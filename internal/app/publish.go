// Package app implements google-play-publisher use cases on top of the Play API.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
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
	VersionCode             int64
	Track                   string
	Status                  string
	Rollout                 float64
	RolloutSet              bool
	Notes                   map[string]string
	ReleaseName             string
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

// Prepared holds a publish operation whose local inputs and policy options
// were validated by Prepare. The fields are unexported so PublishPrepared
// cannot receive a different set of options than the one that was validated.
type Prepared struct {
	opts     PublishOptions
	plan     release.Plan
	artifact artifact.Info
	metadata *artifact.Metadata
	valid    bool
}

// Prepare validates every local input, including policy options and package
// resolution from metadata, without constructing an API client. A malformed
// package, track, status, rollout, notes, AAB, metadata pair or policy fails
// before authentication or any Play call.
func Prepare(opts PublishOptions) (Prepared, error) {
	exists, err := release.ParseExistsPolicy(string(opts.IfExists))
	if err != nil {
		return Prepared{}, err
	}
	opts.IfExists = exists

	if err := validatePublishOptions(opts); err != nil {
		return Prepared{}, err
	}

	if opts.Package == "" && opts.MetadataPath == "" {
		return Prepared{}, exitcode.Errorf(exitcode.Usage, "--package is required")
	}
	if opts.AABPath == "" {
		return Prepared{}, exitcode.Errorf(exitcode.Usage, "--aab is required")
	}

	meta, err := readMetadata(opts)
	if err != nil {
		return Prepared{}, err
	}
	if opts.Package == "" {
		opts.Package = meta.ApplicationID
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
		return Prepared{}, err
	}

	info, err := artifact.Inspect(opts.AABPath)
	if err != nil {
		return Prepared{}, err
	}

	return Prepared{
		opts:     opts,
		plan:     plan,
		artifact: info,
		metadata: meta,
		valid:    true,
	}, nil
}

// Package returns the resolved application id. It is read-only and exists so
// the CLI can report the metadata-resolved package in error output.
func (p Prepared) Package() string { return p.opts.Package }

// validatePublishOptions checks invariants that do not depend on the AAB or
// metadata. Both the CLI and direct app callers go through this.
func validatePublishOptions(opts PublishOptions) error {
	if opts.VersionCode < 0 {
		return exitcode.Errorf(exitcode.Usage, "--version-code must be positive, got %d", opts.VersionCode)
	}
	if opts.DryRun && opts.ChangesNotSentForReview {
		return exitcode.Errorf(exitcode.Usage,
			"--changes-not-sent-for-review cannot be combined with --dry-run")
	}
	switch opts.InReviewBehavior {
	case "", play.InReviewError, play.InReviewReplace:
	default:
		return exitcode.Errorf(exitcode.Usage,
			"invalid in-review behavior %q: want %q or %q",
			opts.InReviewBehavior, play.InReviewError, play.InReviewReplace)
	}
	return nil
}

// Publish uploads an AAB, declares the release on a track and commits the edit.
func (p *Publisher) Publish(ctx context.Context, opts PublishOptions) (PublishResult, error) {
	prepared, err := Prepare(opts)
	if err != nil {
		return PublishResult{}, err
	}
	return p.PublishPrepared(ctx, prepared)
}

// PublishPrepared runs a publish operation for inputs validated by Prepare.
// Passing a zero Prepared is a programming error.
func (p *Publisher) PublishPrepared(ctx context.Context, prepared Prepared) (PublishResult, error) {
	if !prepared.valid {
		return PublishResult{}, exitcode.Errorf(exitcode.Internal,
			"prepared publish state is invalid; call Prepare before PublishPrepared")
	}

	opts := prepared.opts
	plan, info, meta := prepared.plan, prepared.artifact, prepared.metadata

	p.logger.Info("publishing",
		"package", plan.Package,
		"track", plan.Track,
		"status", plan.Status,
		"aab", opts.AABPath,
		"size", info.Size,
	)

	releases, err := p.api.TrackReleases(ctx, plan.Package, plan.Track)
	if err != nil {
		return PublishResult{}, err
	}
	if known := knownVersionCode(opts, meta); known > 0 && versionReleased(releases, known) {
		return p.alreadyReleased(opts, plan, known, info)
	}

	editID, err := p.api.CreateEdit(ctx, plan.Package)
	if err != nil {
		return PublishResult{}, err
	}
	p.logger.Debug("edit created", "editId", editID)

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
	p.logger.Info("bundle uploaded", "versionCode", bundle.VersionCode, "sha256", bundle.SHA256)

	// The file is reopened after inspection, so verify that Play received the
	// bytes that were inspected. A replacement would invalidate size and hash.
	if bundle.SHA256 != "" && !strings.EqualFold(bundle.SHA256, info.SHA256) {
		return PublishResult{}, exitcode.Errorf(exitcode.Internal,
			"uploaded bundle sha256 %s does not match the inspected artifact %s; the file changed during the run",
			bundle.SHA256, info.SHA256)
	}

	if meta != nil && bundle.VersionCode != meta.VersionCode {
		return PublishResult{}, exitcode.Errorf(exitcode.Usage,
			"metadata versionCode %d does not match uploaded versionCode %d",
			meta.VersionCode, bundle.VersionCode)
	}
	if opts.VersionCode > 0 && bundle.VersionCode != opts.VersionCode {
		return PublishResult{}, exitcode.Errorf(exitcode.Usage,
			"versionCode %d does not match uploaded versionCode %d",
			opts.VersionCode, bundle.VersionCode)
	}

	result := PublishResult{
		Package:     plan.Package,
		Track:       plan.Track,
		Status:      string(plan.Status),
		VersionCode: bundle.VersionCode,
		SHA256:      uploadedSHA256(info, bundle),
		EditID:      editID,
	}

	if err := p.api.UpdateTrack(ctx, plan.Package, editID, plan.Track, play.Release{
		VersionCodes: []int64{bundle.VersionCode},
		Status:       string(plan.Status),
		UserFraction: plan.UserFraction,
		ReleaseNotes: playNotes(plan.Notes),
		Name:         opts.ReleaseName,
	}); err != nil {
		return PublishResult{}, err
	}

	if opts.DryRun {
		if err := p.api.ValidateEdit(ctx, plan.Package, editID); err != nil {
			return PublishResult{}, err
		}
		p.logger.Info("dry run validated", "editId", editID)
		result.Review = ReviewValidated
		return result, nil
	}

	review, manual, err := p.commit(ctx, opts, plan.Package, plan.Track, editID, bundle.VersionCode)
	if err != nil {
		return PublishResult{}, err
	}

	committed = true
	result.Review = review

	if manual {
		p.logger.Info("saved for manual review", "versionCode", bundle.VersionCode, "editId", editID)
		return result, exitcode.WithHint(
			exitcode.Errorf(exitcode.ManualAction, "changes saved but not submitted for review"),
			"open Play Console -> Publishing overview and send the changes for review")
	}

	p.logger.Info("published", "versionCode", bundle.VersionCode, "review", review)
	return result, nil
}

// commit applies the edit. Every ambiguous attempt is reconciled against the
// track before the publisher decides that it failed, and the reported review
// outcome always comes from the options actually sent.
func (p *Publisher) commit(
	ctx context.Context,
	opts PublishOptions,
	pkg, track, editID string,
	versionCode int64,
) (review string, manual bool, err error) {
	commitOpts := play.CommitOptions{
		ChangesNotSentForReview: opts.ChangesNotSentForReview,
		InReviewBehavior:        opts.InReviewBehavior,
	}
	transientRetried := false

	for {
		commitErr := p.api.CommitEdit(ctx, pkg, editID, commitOpts)
		if commitErr == nil {
			return reviewOutcome(commitOpts), manual, nil
		}

		// Play refuses to submit automatically for some apps; the documented
		// fallback saves the changes without review and needs a human action.
		if exitcode.From(commitErr) == exitcode.ManualAction && !commitOpts.ChangesNotSentForReview {
			commitOpts.ChangesNotSentForReview = true
			manual = true
			continue
		}

		released, verifyErr := p.isReleased(ctx, pkg, track, versionCode)
		if verifyErr != nil {
			return "", manual, exitcode.WithHint(commitErr, fmt.Sprintf(
				"commit state is unknown for %s track %s version %d (edit %s): check the track before retrying",
				pkg, track, versionCode, editID))
		}
		if released {
			return reviewOutcome(commitOpts), manual, nil
		}

		if exitcode.From(commitErr) == exitcode.Transient && !transientRetried && ctx.Err() == nil {
			transientRetried = true
			continue
		}

		return "", manual, commitErr
	}
}

func (p *Publisher) alreadyReleased(
	opts PublishOptions,
	plan release.Plan,
	versionCode int64,
	info artifact.Info,
) (PublishResult, error) {
	if opts.IfExists == release.ExistsSkip {
		p.logger.Info("skipped: version already released", "versionCode", versionCode, "track", plan.Track)
		return PublishResult{
			Package:     plan.Package,
			Track:       plan.Track,
			Status:      string(plan.Status),
			VersionCode: versionCode,
			SHA256:      info.SHA256,
			Review:      ReviewSkipped,
		}, nil
	}

	return PublishResult{}, exitcode.WithHint(
		exitcode.Errorf(exitcode.Conflict,
			"versionCode %d is already on track %s", versionCode, plan.Track),
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
	if opts.Package != "" {
		if err := meta.VerifyPackage(opts.Package); err != nil {
			return nil, err
		}
	}
	// Both values describe the same artifact, so a contradiction is a local
	// input error. Reject it before an edit is created or a token is fetched.
	if opts.VersionCode > 0 && meta.VersionCode != opts.VersionCode {
		return nil, exitcode.Errorf(exitcode.Usage,
			"metadata versionCode %d does not match --version-code %d",
			meta.VersionCode, opts.VersionCode)
	}
	return &meta, nil
}

// uploadedSHA256 returns the hash of the bytes Play received, falling back to
// the locally inspected value when the API omits it.
// knownVersionCode returns the version code declared by the caller, if any.
// --version-code wins over metadata because it is the explicit declaration.
func knownVersionCode(opts PublishOptions, meta *artifact.Metadata) int64 {
	if opts.VersionCode > 0 {
		return opts.VersionCode
	}
	if meta != nil {
		return meta.VersionCode
	}
	return 0
}

func uploadedSHA256(info artifact.Info, bundle play.BundleInfo) string {
	if bundle.SHA256 != "" {
		return bundle.SHA256
	}
	return info.SHA256
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

func reviewOutcome(opts play.CommitOptions) string {
	if opts.ChangesNotSentForReview {
		return ReviewNotSent
	}
	if opts.InReviewBehavior == play.InReviewReplace {
		return ReviewReplaced
	}
	return ReviewSent
}
