// Package app implements google-play-publisher use cases on top of the Play API.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
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
	ChangelogDir            string
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
	VersionName string
}

// Publisher runs the publish use case.
type Publisher struct {
	api          play.API
	logger       *slog.Logger
	recoveryBase context.Context
}

// PublisherOption configures a Publisher.
type PublisherOption func(*Publisher)

// WithRecoveryBase sets the parent context of the bounded commit-recovery
// read. It should carry signal cancellation but no operation deadline: the
// publisher adds its own recovery budget. When the base is canceled, an
// already running recovery stops too.
func WithRecoveryBase(ctx context.Context) PublisherOption {
	return func(p *Publisher) {
		if ctx != nil {
			p.recoveryBase = ctx
		}
	}
}

// NewPublisher creates a Publisher. A nil logger discards logs.
func NewPublisher(api play.API, logger *slog.Logger, opts ...PublisherOption) *Publisher {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	publisher := &Publisher{
		api:          api,
		logger:       logger,
		recoveryBase: context.Background(),
	}
	for _, opt := range opts {
		opt(publisher)
	}
	return publisher
}

// Prepared holds a publish operation whose local inputs and policy options
// were validated by Prepare. The fields are unexported so PublishPrepared
// cannot receive a different set of options than the one that was validated.
type Prepared struct {
	opts     PublishOptions
	plan     release.Plan
	bundle   *artifact.Bundle
	metadata *artifact.Metadata
	state    *preparedState
}

// preparedState is shared by copies of Prepared so closing or consuming one
// copy is visible to every copy.
type preparedState struct {
	mu       sync.Mutex
	valid    bool
	closed   bool
	consumed bool
}

// Close closes the opened AAB. PublishPrepared closes it after the upload;
// callers that abandon a Prepared value must close it themselves. Close is
// idempotent.
func (p Prepared) Close() error {
	if p.state != nil {
		p.state.mu.Lock()
		defer p.state.mu.Unlock()
		if p.state.closed {
			return nil
		}
		p.state.closed = true
	}
	if p.bundle == nil {
		return nil
	}
	return p.bundle.Close()
}

// begin marks the operation as consumed. It rejects invalid, closed and reused
// values before any network call so a programming error cannot create an edit.
func (p Prepared) begin() error {
	if p.state == nil {
		return exitcode.Errorf(exitcode.Internal,
			"prepared publish state is invalid; call Prepare before PublishPrepared")
	}

	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	switch {
	case !p.state.valid:
		return exitcode.Errorf(exitcode.Internal,
			"prepared publish state is invalid; call Prepare before PublishPrepared")
	case p.state.closed:
		return exitcode.Errorf(exitcode.Internal,
			"prepared publish state is closed; call Prepare again")
	case p.state.consumed:
		return exitcode.Errorf(exitcode.Internal,
			"prepared publish state was already used; call Prepare again")
	}
	p.state.consumed = true
	return nil
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

	if opts.AABPath == "" {
		return Prepared{}, exitcode.Errorf(exitcode.Usage, "--aab is required")
	}

	meta, err := readMetadata(opts)
	if err != nil {
		return Prepared{}, err
	}

	bundle, err := artifact.Open(opts.AABPath)
	if err != nil {
		return Prepared{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = bundle.Close()
		}
	}()

	resolved, err := validateIdentity(bundle.Identity, opts.Package, opts.VersionCode, meta)
	if err != nil {
		return Prepared{}, err
	}
	opts.Package = resolved.Package
	opts.VersionCode = resolved.VersionCode

	if opts.ChangelogDir != "" && len(opts.Notes) > 0 {
		return Prepared{}, exitcode.Errorf(exitcode.Usage,
			"release notes cannot come from both --changelog-dir and notes passed by a caller")
	}
	notes := opts.Notes
	if opts.ChangelogDir != "" {
		notes, err = readChangelogDir(opts.ChangelogDir, opts.VersionCode)
		if err != nil {
			return Prepared{}, err
		}
	}

	plan, err := release.NewPlan(release.Input{
		Package:    opts.Package,
		Track:      opts.Track,
		Status:     opts.Status,
		Rollout:    opts.Rollout,
		RolloutSet: opts.RolloutSet,
		Notes:      notes,
	})
	if err != nil {
		return Prepared{}, err
	}

	closed = true
	return Prepared{
		opts:     opts,
		plan:     plan,
		bundle:   bundle,
		metadata: meta,
		state:    &preparedState{valid: true},
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
	if err := prepared.begin(); err != nil {
		return PublishResult{}, err
	}

	opts := prepared.opts
	plan, meta := prepared.plan, prepared.metadata
	info := prepared.bundle.Info
	defer func() { _ = prepared.Close() }()

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
		return p.alreadyReleased(opts, plan, known, info, prepared.bundle.Identity.VersionName)
	}

	editID, err := p.api.CreateEdit(ctx, plan.Package)
	if err != nil {
		return PublishResult{}, err
	}
	p.logger.Debug("edit created", "editId", editID)

	// keepEdit is false until Play confirms that the edit was saved. An
	// unknown commit keeps the edit too: deleting it could discard a release
	// that the server already applied.
	keepEdit := false
	defer func() {
		if !keepEdit {
			p.deleteEdit(plan.Package, editID)
		}
	}()

	if err := prepared.bundle.Rewind(); err != nil {
		return PublishResult{}, exitcode.Wrap(exitcode.Usage, err)
	}

	bundle, err := p.api.UploadBundle(ctx, plan.Package, editID, prepared.bundle.File, info.Size)
	if err != nil {
		return PublishResult{}, err
	}
	p.logger.Info("bundle uploaded", "versionCode", bundle.VersionCode, "sha256", bundle.SHA256)

	// The upload reads from the handle that was inspected. Keep verifying the
	// returned hash so a transport-level mismatch still stops the release.
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
		VersionName: prepared.bundle.Identity.VersionName,
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

	outcome := p.commit(ctx, opts, plan, editID, bundle.VersionCode)
	switch outcome.kind {
	case commitCommitted:
		keepEdit = true
		result.Review = outcome.review
		if outcome.manual {
			p.logger.Info("saved for manual review", "versionCode", bundle.VersionCode, "editId", editID)
			return result, exitcode.WithHint(
				exitcode.Errorf(exitcode.ManualAction, "changes saved but not submitted for review"),
				"open Play Console -> Publishing overview and send the changes for review")
		}
		p.logger.Info("published", "versionCode", bundle.VersionCode, "review", outcome.review)
		return result, nil
	case commitUnknown:
		keepEdit = true
		p.logger.Warn("commit state is unknown", "editId", editID, "error", outcome.err)
		return PublishResult{}, outcome.err
	default:
		return PublishResult{}, outcome.err
	}
}

// commitOutcome is the verified result of applying an edit.
type commitOutcome int

const (
	// commitNotCommitted means Play rejected the edit or never applied it.
	commitNotCommitted commitOutcome = iota
	// commitCommitted means Play confirms that the release was saved.
	commitCommitted
	// commitUnknown means the edit may have been applied, but its state could
	// not be verified.
	commitUnknown
)

// commitResult carries the verified outcome of applying an edit.
type commitResult struct {
	kind   commitOutcome
	review string
	manual bool
	err    error
}

// commitRecoveryBudget bounds the reconciliation read after the operation
// deadline expired. See docs/05-errors.md §4.
const commitRecoveryBudget = 15 * time.Second

// commit applies the edit. Definitive Play rejections are returned unchanged.
// Every ambiguous attempt is reconciled against the track before the publisher
// decides what happened, and the reported review outcome comes from the options
// actually sent plus the observed release lifecycle state.
func (p *Publisher) commit(
	ctx context.Context,
	opts PublishOptions,
	plan release.Plan,
	editID string,
	versionCode int64,
) commitResult {
	commitOpts := play.CommitOptions{
		ChangesNotSentForReview: opts.ChangesNotSentForReview,
		InReviewBehavior:        opts.InReviewBehavior,
	}
	manual := false
	transientRetried := false

	for {
		commitErr := p.api.CommitEdit(ctx, plan.Package, editID, commitOpts)
		if commitErr == nil {
			return commitResult{kind: commitCommitted, review: reviewOutcome(commitOpts), manual: manual}
		}

		// Play refuses to submit automatically for some apps; the documented
		// fallback saves the changes without review and needs a human action.
		if exitcode.From(commitErr) == exitcode.ManualAction && !commitOpts.ChangesNotSentForReview {
			commitOpts.ChangesNotSentForReview = true
			manual = true
			continue
		}

		// A definitive rejection stays an error even when another operation
		// happens to put the same version code on the track.
		if !ambiguousCommit(commitErr) {
			return commitResult{kind: commitNotCommitted, manual: manual, err: commitErr}
		}

		released, state, verifyErr := p.reconcileCommit(ctx, plan, versionCode)
		if verifyErr != nil {
			return commitResult{
				kind:   commitUnknown,
				manual: manual,
				err:    unknownCommitStateError(plan, editID, versionCode, verifyErr),
			}
		}
		if released {
			if state == "" || state == play.LifecycleUnspecified {
				p.logger.Warn("release state does not confirm the review outcome; reporting it from the commit options",
					"versionCode", versionCode, "state", state)
			}
			review, needsManual := observedReview(plan, commitOpts, state, manual)
			return commitResult{kind: commitCommitted, review: review, manual: needsManual}
		}

		if exitcode.From(commitErr) == exitcode.Transient && !transientRetried && ctx.Err() == nil {
			transientRetried = true
			continue
		}

		return commitResult{kind: commitNotCommitted, manual: manual, err: commitErr}
	}
}

// ambiguousCommit reports whether a commit failure may hide an applied edit.
// Only failures typed by the Play adapter as transport or server-side failures
// are reconciled; Play rejections and token retrieval failures are definitive.
func ambiguousCommit(err error) bool {
	if !play.IsAmbiguous(err) {
		return false
	}
	switch exitcode.From(err) {
	case exitcode.Transient, exitcode.Internal, exitcode.Interrupted:
		return true
	default:
		return false
	}
}

// observedReview derives the reported review outcome from the options actually
// sent and the release lifecycle state seen on the track. A release that exists
// but was not submitted for review must not be reported as sent.
func observedReview(plan release.Plan, commitOpts play.CommitOptions, state string, manual bool) (string, bool) {
	review := reviewOutcome(commitOpts)
	if state != play.LifecycleNotSentForReview || commitOpts.ChangesNotSentForReview {
		return review, manual
	}
	if plan.Status == release.StatusDraft {
		// The caller explicitly asked for a draft release; nothing else to do.
		return ReviewNotSent, manual
	}
	return ReviewNotSent, true
}

// reconcileCommit reads the track after an ambiguous commit. If the operation
// deadline has already expired, the read runs under a separate short budget so
// that a lost response can still be recognized. Signal cancellation keeps the
// interrupted semantics and does not start recovery.
func (p *Publisher) reconcileCommit(ctx context.Context, plan release.Plan, versionCode int64) (bool, string, error) {
	// A signal-canceled run must not perform another request and must keep the
	// interrupted semantics; the caller reports the state as unknown.
	if errors.Is(ctx.Err(), context.Canceled) {
		return false, "", ctx.Err()
	}

	released, state, err := p.releaseState(ctx, plan, versionCode)
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return released, state, err
	}

	base := p.recoveryBase
	if base == nil {
		base = context.Background()
	}
	recoveryCtx, cancel := context.WithTimeout(base, commitRecoveryBudget)
	defer cancel()
	p.logger.Warn("reconciliation after the operation deadline",
		"error", err, "budget", commitRecoveryBudget.String())
	return p.releaseState(recoveryCtx, plan, versionCode)
}

// releaseState reports whether versionCode is on the track and, when it is,
// the lifecycle state of the release that carries it.
func (p *Publisher) releaseState(ctx context.Context, plan release.Plan, versionCode int64) (bool, string, error) {
	releases, err := p.api.TrackReleases(ctx, plan.Package, plan.Track)
	if err != nil {
		return false, "", err
	}
	for _, rel := range releases {
		for _, code := range rel.VersionCodes {
			if code == versionCode {
				return true, rel.State, nil
			}
		}
	}
	return false, "", nil
}

// unknownCommitStateError reports an unverified commit without claiming that
// the publication failed. The edit is kept because it may already be closed.
func unknownCommitStateError(plan release.Plan, editID string, versionCode int64, cause error) error {
	code := exitcode.Transient
	if errors.Is(cause, context.Canceled) {
		code = exitcode.Interrupted
	}
	err := exitcode.Wrap(code, fmt.Errorf(
		"commit state is unknown for %s track %s version %d (edit %s): %w",
		plan.Package, plan.Track, versionCode, editID, cause))
	return exitcode.WithHint(err,
		"the release may be saved; check the track before retrying; the edit was kept")
}

func (p *Publisher) alreadyReleased(
	opts PublishOptions,
	plan release.Plan,
	versionCode int64,
	info artifact.Info,
	versionName string,
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
			VersionName: versionName,
		}, nil
	}

	return PublishResult{}, exitcode.WithHint(
		exitcode.Errorf(exitcode.Conflict,
			"versionCode %d is already on track %s", versionCode, plan.Track),
		"bump versionCode and rebuild the AAB, or pass --if-exists=skip",
	)
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
