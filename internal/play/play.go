// Package play is the adapter for the Google Play Developer Publishing API.
package play

import (
	"context"
	"io"
)

// API is the subset of the Google Play Publishing API used by gplay.
type API interface {
	CreateEdit(ctx context.Context, pkg string) (editID string, err error)
	DeleteEdit(ctx context.Context, pkg, editID string) error
	UploadBundle(ctx context.Context, pkg, editID string, r io.Reader, size int64) (BundleInfo, error)
	UpdateTrack(ctx context.Context, pkg, editID, track string, rel Release) error
	ValidateEdit(ctx context.Context, pkg, editID string) error
	CommitEdit(ctx context.Context, pkg, editID string, opts CommitOptions) error
	TrackReleases(ctx context.Context, pkg, track string) ([]ReleaseSummary, error)
}

// Release statuses accepted by the API.
const (
	StatusDraft      = "draft"
	StatusInProgress = "inProgress"
	StatusHalted     = "halted"
	StatusCompleted  = "completed"
)

// Commit behavior when changes are already in review.
const (
	InReviewError   = "ERROR_IF_IN_REVIEW"
	InReviewReplace = "CANCEL_IN_REVIEW_AND_SUBMIT"
)

// BundleInfo describes an uploaded Android App Bundle.
type BundleInfo struct {
	VersionCode int64
	SHA1        string
	SHA256      string
}

// LocalizedText is a release note for one locale.
type LocalizedText struct {
	Language string
	Text     string
}

// Release is a desired track release.
type Release struct {
	VersionCodes []int64
	Status       string
	UserFraction float64
	ReleaseNotes []LocalizedText
	Name         string
}

// CommitOptions controls edits.commit.
type CommitOptions struct {
	ChangesNotSentForReview bool
	InReviewBehavior        string
}

// ReleaseSummary is a read-only view of one release in a track.
type ReleaseSummary struct {
	Track        string
	Name         string
	State        string
	VersionCodes []int64
}
