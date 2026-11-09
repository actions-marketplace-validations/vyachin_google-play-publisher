// Package play is the adapter for the Google Play Developer Publishing API.
package play

import (
	"context"
	"io"
)

// API is the subset of the Google Play Publishing API used by google-play-publisher.
type API interface {
	CreateEdit(ctx context.Context, pkg string) (editID string, err error)
	DeleteEdit(ctx context.Context, pkg, editID string) error
	UploadBundle(ctx context.Context, pkg, editID string, r io.Reader, size int64) (BundleInfo, error)
	UpdateTrack(ctx context.Context, pkg, editID, track string, rel Release) error
	TrackEdit(ctx context.Context, pkg, editID, track string) (TrackState, error)
	UpdateListing(ctx context.Context, pkg, editID string, listing Listing) error
	StoreListings(ctx context.Context, pkg, editID string) ([]Listing, error)
	DeleteAllImages(ctx context.Context, pkg, editID, language, imageType string) error
	UploadImage(ctx context.Context, pkg, editID, language, imageType, path string) error
	StoreImages(ctx context.Context, pkg, editID, language, imageType string) ([]Image, error)
	DownloadImage(ctx context.Context, url string) ([]byte, error)
	UpdateTesters(ctx context.Context, pkg, editID string, testers Testers) error
	TesterGroups(ctx context.Context, pkg, editID, track string) (Testers, error)
	AllTracks(ctx context.Context, pkg, editID string) ([]TrackState, error)
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

// Release lifecycle states reported by applications.tracks.releases.list.
const (
	LifecycleUnspecified      = "RELEASE_LIFECYCLE_STATE_UNSPECIFIED"
	LifecycleDraft            = "RELEASE_LIFECYCLE_STATE_DRAFT"
	LifecycleNotSentForReview = "RELEASE_LIFECYCLE_STATE_NOT_SENT_FOR_REVIEW"
	LifecycleInReview         = "RELEASE_LIFECYCLE_STATE_IN_REVIEW"
	LifecyclePublished        = "RELEASE_LIFECYCLE_STATE_PUBLISHED"
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

// CountryTargeting restricts a release to a set of countries. Play accepts it
// only for an inProgress release on the production track.
type CountryTargeting struct {
	Countries          []string
	IncludeRestOfWorld bool
}

// Release is a desired track release. The optional fields are omitted when
// they are zero, so an empty Release keeps Play's previous values.
type Release struct {
	VersionCodes        []int64
	Status              string
	UserFraction        float64
	ReleaseNotes        []LocalizedText
	Name                string
	InAppUpdatePriority int64
	CountryTargeting    *CountryTargeting
}

// TrackState is the current content of a track inside an edit.
type TrackState struct {
	Track    string
	Releases []Release
}

// Listing is one localized store listing.
type Listing struct {
	Language         string
	Title            string
	ShortDescription string
	FullDescription  string
	Video            string
}

// Image is one store image listed inside an edit. URL serves the image
// preview; the adapter appends the full-size parameter before downloading.
type Image struct {
	ID     string
	SHA256 string
	URL    string
}

// Testers is the tester configuration of one track. Play accepts Google Groups
// as email addresses.
type Testers struct {
	Track        string
	GoogleGroups []string
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
